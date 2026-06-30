package auth

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func newTestIssuer() *Issuer {
	return NewIssuer([]byte("test-secret"), 15*time.Minute, time.Hour, 4*time.Hour)
}

func TestSignalingTokenRoundTrip(t *testing.T) {
	i := newTestIssuer()
	room := uuid.NewString()

	tok, err := i.IssueSignaling(room, "agent")
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	claims, err := i.ParseSignaling(tok)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if claims.Room != room || claims.Role != "agent" {
		t.Fatalf("unexpected claims: room=%s role=%s", claims.Room, claims.Role)
	}
	if claims.Kind != KindSignaling {
		t.Fatalf("expected signaling kind, got %s", claims.Kind)
	}
}

func TestParseSignalingRejectsAccessToken(t *testing.T) {
	i := newTestIssuer()
	// An access token must not be accepted as a signaling token.
	access, _, err := i.Issue(KindAccess, uuid.New(), uuid.New(), "agent")
	if err != nil {
		t.Fatalf("issue access: %v", err)
	}
	if _, err := i.ParseSignaling(access); err == nil {
		t.Fatal("expected access token to be rejected by ParseSignaling")
	}
}

func TestParseSignalingRejectsWrongSecret(t *testing.T) {
	i := newTestIssuer()
	other := NewIssuer([]byte("different-secret"), 15*time.Minute, time.Hour, 4*time.Hour)

	tok, _ := i.IssueSignaling(uuid.NewString(), "phone")
	if _, err := other.ParseSignaling(tok); err == nil {
		t.Fatal("expected token signed with a different secret to be rejected")
	}
}
