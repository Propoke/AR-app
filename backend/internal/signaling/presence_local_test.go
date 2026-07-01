package signaling

import "testing"

func TestLocalPresenceReportsCounterpart(t *testing.T) {
	p := NewLocalPresence()
	room := "r1"

	// Agent joins first: no one else present.
	if others := p.Join(room, RoleAgent); len(others) != 0 {
		t.Fatalf("agent first: expected no others, got %v", others)
	}
	// Phone joins: should see the agent.
	others := p.Join(room, RolePhone)
	if len(others) != 1 || others[0] != RoleAgent {
		t.Fatalf("phone join: expected [agent], got %v", others)
	}
}

func TestLocalPresenceLeaveClears(t *testing.T) {
	p := NewLocalPresence()
	room := "r2"
	p.Join(room, RoleAgent)
	p.Join(room, RolePhone)

	// The phone is still present, so the room is not empty yet.
	if empty := p.Leave(room, RoleAgent); empty {
		t.Fatal("room should not be reported empty while the phone is still present")
	}
	// After the agent leaves, a rejoining phone sees no counterpart.
	if others := p.Join(room, RolePhone); len(others) != 0 {
		t.Fatalf("after agent left: expected no others, got %v", others)
	}
}

// TestLocalPresenceLeaveReportsEmptyWhenLastOccupantLeaves is the core signal
// session-lifecycle bookkeeping depends on (see signaling.Hub.OnRoomEnded):
// Leave must report true exactly once the room's last occupant departs, and
// false for every leave before that.
func TestLocalPresenceLeaveReportsEmptyWhenLastOccupantLeaves(t *testing.T) {
	p := NewLocalPresence()
	room := "r-lifecycle"
	p.Join(room, RoleAgent)
	p.Join(room, RolePhone)

	if empty := p.Leave(room, RoleAgent); empty {
		t.Fatal("leaving with a counterpart still present must report not-empty")
	}
	if empty := p.Leave(room, RolePhone); !empty {
		t.Fatal("leaving as the last occupant must report empty")
	}
}

func TestLocalPresenceLeaveOfUnknownRoomReportsEmpty(t *testing.T) {
	p := NewLocalPresence()
	if empty := p.Leave("never-joined", RoleAgent); !empty {
		t.Fatal("leaving a room with no tracked presence should report empty")
	}
}

func TestLocalPresenceToleratesReconnectOverlap(t *testing.T) {
	p := NewLocalPresence()
	room := "r3"
	p.Join(room, RoleAgent)
	p.Join(room, RoleAgent) // transient double (reconnect before reap)
	if empty := p.Leave(room, RoleAgent); empty {
		t.Fatal("one agent connection remains after a single leave; must not report empty")
	}
	// One agent connection remains, so the phone should still see it.
	if others := p.Join(room, RolePhone); len(others) != 1 || others[0] != RoleAgent {
		t.Fatalf("expected agent still present after one leave, got %v", others)
	}
}
