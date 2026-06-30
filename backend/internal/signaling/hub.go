// Package signaling relays WebRTC SDP/ICE and early annotation messages between
// the two peers of a session over WebSockets.
//
// Phase 1 provides an in-memory, single-instance hub: a room holds at most the
// two peers (agent + phone) and forwards each peer's messages to the other.
// Scaling to multiple backend instances will add a Redis pub/sub fan-out layer
// (Phase 2); the Peer/Room abstraction here is designed to make that swap local.
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

// Peer is a connected participant that can receive raw message frames.
type Peer interface {
	ID() string
	Role() Role
	// Send delivers a frame to the peer. It must not block the hub; implementations
	// should buffer and drop on overflow.
	Send(frame []byte) bool
}

// Room holds the peers of a single session keyed by role.
type Room struct {
	mu    sync.Mutex
	peers map[Role]Peer
}

// Hub manages all active rooms.
type Hub struct {
	mu    sync.RWMutex
	rooms map[string]*Room
}

// NewHub constructs an empty Hub.
func NewHub() *Hub {
	return &Hub{rooms: make(map[string]*Room)}
}

// Join adds a peer to a room, replacing any existing peer with the same role
// (e.g. a reconnect). It returns the room.
func (h *Hub) Join(roomID string, p Peer) *Room {
	h.mu.Lock()
	room := h.rooms[roomID]
	if room == nil {
		room = &Room{peers: make(map[Role]Peer)}
		h.rooms[roomID] = room
	}
	h.mu.Unlock()

	room.mu.Lock()
	room.peers[p.Role()] = p
	room.mu.Unlock()
	return room
}

// Leave removes a peer from a room (only if it is still the current occupant of
// its role) and garbage-collects empty rooms.
func (h *Hub) Leave(roomID string, p Peer) {
	h.mu.Lock()
	room := h.rooms[roomID]
	h.mu.Unlock()
	if room == nil {
		return
	}

	room.mu.Lock()
	if cur, ok := room.peers[p.Role()]; ok && cur.ID() == p.ID() {
		delete(room.peers, p.Role())
	}
	empty := len(room.peers) == 0
	room.mu.Unlock()

	if empty {
		h.mu.Lock()
		// Re-check under the write lock in case someone rejoined meanwhile.
		if r := h.rooms[roomID]; r != nil {
			r.mu.Lock()
			if len(r.peers) == 0 {
				delete(h.rooms, roomID)
			}
			r.mu.Unlock()
		}
		h.mu.Unlock()
	}
}

// Relay forwards a frame from sender to the other peer in the room. It reports
// whether a counterpart existed to receive it.
func (h *Hub) Relay(roomID string, sender Peer, frame []byte) bool {
	h.mu.RLock()
	room := h.rooms[roomID]
	h.mu.RUnlock()
	if room == nil {
		return false
	}

	room.mu.Lock()
	defer room.mu.Unlock()
	delivered := false
	for role, peer := range room.peers {
		if role == sender.Role() {
			continue
		}
		if peer.Send(frame) {
			delivered = true
		}
	}
	return delivered
}

// Counterpart returns the other peer in the room, if present.
func (h *Hub) Counterpart(roomID string, self Peer) (Peer, bool) {
	h.mu.RLock()
	room := h.rooms[roomID]
	h.mu.RUnlock()
	if room == nil {
		return nil, false
	}
	room.mu.Lock()
	defer room.mu.Unlock()
	for role, peer := range room.peers {
		if role != self.Role() {
			return peer, true
		}
	}
	return nil, false
}
