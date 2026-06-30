package turn

import (
	"strings"
	"testing"
	"time"
)

func TestCredentialsFormat(t *testing.T) {
	m := NewMinter("secret", []string{"turn:turn.example:3478?transport=udp"}, time.Hour)
	servers := m.Credentials("session-123")
	if len(servers) != 1 {
		t.Fatalf("expected 1 ICE server, got %d", len(servers))
	}
	s := servers[0]
	// Username is "<expiry-unix>:<identifier>".
	parts := strings.SplitN(s.Username, ":", 2)
	if len(parts) != 2 || parts[1] != "session-123" {
		t.Fatalf("unexpected username %q", s.Username)
	}
	if s.Credential == "" {
		t.Fatal("credential must not be empty")
	}
}

func TestCredentialsDisabledWithoutSecret(t *testing.T) {
	m := NewMinter("", []string{"turn:x"}, time.Hour)
	if m.Credentials("id") != nil {
		t.Fatal("expected nil ICE servers when no secret configured")
	}
}
