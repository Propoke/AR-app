// Package signaling relays WebRTC SDP/ICE and early annotation messages between
// the two peers of a session over WebSockets.
//
// Delivery is abstracted behind Fanout (frame distribution) and Presence (which
// roles are in a room). The default LocalFanout/LocalPresence keep everything in
// one process; the Redis implementations add cross-instance delivery so the
// backend can run multiple replicas. Each instance always holds only its own
// peers' sockets; frames destined for a peer on another instance travel via the
// fanout.
package signaling

import (
	"sync"
)

// Role identifies which side of a session a peer is.
type Role string

const (
	RoleAgent Role = "agent"
	RolePhone Role = "phone"
)

// opposite returns the other role in a session.
func opposite(r Role) Role {
	if r == RoleAgent {
		return RolePhone
	}
	return RoleAgent
}

// Peer is a connected participant that can receive raw message frames.
type Peer interface {
	ID() string
	Role() Role
	// Send delivers a frame to the peer. It must not block the hub; implementations
	// should buffer and drop on overflow.
	Send(frame []byte) bool
}

// Hub manages local peers and routes frames through a Fanout, using Presence to
// know when both sides of a room are connected.
type Hub struct {
	mu          sync.RWMutex
	rooms       map[string]map[Role]Peer // local peers only
	fanout      Fanout
	presence    Presence
	onRoomEnded func(room string)
}

// NewHub constructs a single-process Hub (local fanout + presence).
func NewHub() *Hub {
	return NewHubWith(NewLocalFanout(), NewLocalPresence())
}

// NewHubWith constructs a Hub with the given Fanout and Presence (e.g. Redis-backed
// for multi-instance deployments).
func NewHubWith(fanout Fanout, presence Presence) *Hub {
	h := &Hub{
		rooms:    make(map[string]map[Role]Peer),
		fanout:   fanout,
		presence: presence,
	}
	fanout.Start(h.deliverLocal)
	return h
}

// Join registers a peer locally and, if its counterpart is present anywhere,
// notifies both sides that the room is ready (so the agent sends its offer).
func (h *Hub) Join(room string, p Peer) {
	h.mu.Lock()
	if h.rooms[room] == nil {
		h.rooms[room] = make(map[Role]Peer)
	}
	h.rooms[room][p.Role()] = p
	h.mu.Unlock()

	others := h.presence.Join(room, p.Role())
	for _, other := range others {
		if other == opposite(p.Role()) {
			ready := mustMarshal(envelope{Type: "peer-ready"})
			p.Send(ready)                                     // the newcomer (local)
			h.fanout.Publish(room, opposite(p.Role()), ready) // its counterpart (local or remote)
			break
		}
	}
}

// Leave removes a peer (if it is still the current occupant of its role) and
// updates presence. If this was the room's last occupant across all instances
// (per Presence, which is authoritative even under the Redis fanout), the
// OnRoomEnded callback fires — this is the "session has ended" signal.
func (h *Hub) Leave(room string, p Peer) {
	h.mu.Lock()
	if peers := h.rooms[room]; peers != nil {
		if cur, ok := peers[p.Role()]; ok && cur.ID() == p.ID() {
			delete(peers, p.Role())
		}
		if len(peers) == 0 {
			delete(h.rooms, room)
		}
	}
	onEnded := h.onRoomEnded
	h.mu.Unlock()

	empty := h.presence.Leave(room, p.Role())
	if empty && onEnded != nil {
		onEnded(room)
	}
}

// OnRoomEnded registers a callback fired once, when a room's last occupant
// leaves (see Leave). Optional; used to mark a session ended in the database.
// Safe to call at any time; takes effect for leaves that happen after it
// returns.
func (h *Hub) OnRoomEnded(fn func(room string)) {
	h.mu.Lock()
	h.onRoomEnded = fn
	h.mu.Unlock()
}

// Relay forwards a frame from sender to the opposite peer in the room, wherever
// that peer is connected.
func (h *Hub) Relay(room string, sender Peer, frame []byte) {
	h.fanout.Publish(room, opposite(sender.Role()), frame)
}

// deliverLocal sends a frame to the local peer of targetRole in room, if present.
// It is the callback invoked by the fanout on every instance.
func (h *Hub) deliverLocal(room string, targetRole Role, frame []byte) {
	h.mu.RLock()
	var peer Peer
	if peers := h.rooms[room]; peers != nil {
		peer = peers[targetRole]
	}
	h.mu.RUnlock()
	if peer != nil {
		peer.Send(frame)
	}
}

// RoomCount returns the number of active local rooms (for metrics).
func (h *Hub) RoomCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.rooms)
}
