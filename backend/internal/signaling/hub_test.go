package signaling

import (
	"sync"
	"testing"
)

// fakePeer is a minimal Peer for hub-level tests that don't need a real
// WebSocket connection.
type fakePeer struct {
	id   string
	role Role
	mu   sync.Mutex
	sent [][]byte
}

func (p *fakePeer) ID() string { return p.id }
func (p *fakePeer) Role() Role { return p.role }
func (p *fakePeer) Send(frame []byte) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.sent = append(p.sent, frame)
	return true
}

// TestHubOnRoomEndedFiresOnlyWhenBothPeersLeave is the regression test for
// session-lifecycle bookkeeping (see session.Service.MarkEnded, wired via
// cmd/server's hub.OnRoomEnded callback): the "room ended" signal must not
// fire while a counterpart is still present, and must fire exactly once when
// the last peer of the room departs.
func TestHubOnRoomEndedFiresOnlyWhenBothPeersLeave(t *testing.T) {
	h := NewHub()

	var mu sync.Mutex
	var endedRooms []string
	h.OnRoomEnded(func(room string) {
		mu.Lock()
		defer mu.Unlock()
		endedRooms = append(endedRooms, room)
	})

	const room = "room-1"
	agent := &fakePeer{id: "a1", role: RoleAgent}
	phone := &fakePeer{id: "p1", role: RolePhone}

	h.Join(room, agent)
	h.Join(room, phone)

	h.Leave(room, agent)
	mu.Lock()
	got := len(endedRooms)
	mu.Unlock()
	if got != 0 {
		t.Fatalf("OnRoomEnded must not fire while the phone is still present; fired %d times", got)
	}

	h.Leave(room, phone)
	mu.Lock()
	defer mu.Unlock()
	if len(endedRooms) != 1 || endedRooms[0] != room {
		t.Fatalf("expected OnRoomEnded to fire exactly once for %q, got %v", room, endedRooms)
	}
}

// TestHubOnRoomEndedFiresForAbandonedSingleOccupantRoom covers the case where
// only one peer ever joins (e.g. the agent connects to signaling and waits,
// but the phone never redeems/connects) and then leaves — the room still
// "ends" from the Hub's perspective, even though it never reached peer-ready.
func TestHubOnRoomEndedFiresForAbandonedSingleOccupantRoom(t *testing.T) {
	h := NewHub()
	var mu sync.Mutex
	fired := 0
	h.OnRoomEnded(func(string) {
		mu.Lock()
		fired++
		mu.Unlock()
	})

	agent := &fakePeer{id: "a1", role: RoleAgent}
	h.Join("room-2", agent)
	h.Leave("room-2", agent)

	mu.Lock()
	defer mu.Unlock()
	if fired != 1 {
		t.Fatalf("expected OnRoomEnded to fire once for an abandoned single-occupant room, got %d", fired)
	}
}

// TestHubWithoutOnRoomEndedDoesNotPanic confirms Leave is safe when no
// callback has been registered (the common case for the DB-free dev relay).
func TestHubWithoutOnRoomEndedDoesNotPanic(t *testing.T) {
	h := NewHub()
	agent := &fakePeer{id: "a1", role: RoleAgent}
	h.Join("room-3", agent)
	h.Leave("room-3", agent) // must not panic
}
