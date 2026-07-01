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

	p.Leave(room, RoleAgent)
	// After the agent leaves, a rejoining phone sees no counterpart.
	if others := p.Join(room, RolePhone); len(others) != 0 {
		t.Fatalf("after agent left: expected no others, got %v", others)
	}
}

func TestLocalPresenceToleratesReconnectOverlap(t *testing.T) {
	p := NewLocalPresence()
	room := "r3"
	p.Join(room, RoleAgent)
	p.Join(room, RoleAgent) // transient double (reconnect before reap)
	p.Leave(room, RoleAgent)
	// One agent connection remains, so the phone should still see it.
	if others := p.Join(room, RolePhone); len(others) != 1 || others[0] != RoleAgent {
		t.Fatalf("expected agent still present after one leave, got %v", others)
	}
}
