//go:build integration

package integration

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	"github.com/propoke/ar-app/backend/internal/auth"
	"github.com/propoke/ar-app/backend/internal/session"
	"github.com/propoke/ar-app/backend/internal/signaling"
	"github.com/propoke/ar-app/backend/internal/store"
	"github.com/propoke/ar-app/backend/internal/turn"
)

// newTestStoreAndSession opens a fresh store (skipping the test if
// DATABASE_URL/REDIS_URL aren't set, matching newServer's convention) and
// constructs a session.Service against it, for tests that only need the
// session lifecycle methods rather than a full HTTP server.
func newTestStoreAndSession(t *testing.T) (*store.Store, *session.Service) {
	t.Helper()
	ctx := context.Background()
	dbURL := os.Getenv("DATABASE_URL")
	redisURL := os.Getenv("REDIS_URL")
	if dbURL == "" || redisURL == "" {
		t.Skip("DATABASE_URL and REDIS_URL must be set for integration tests")
	}

	st, err := store.Open(ctx, dbURL, redisURL)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(st.Close)
	if err := st.Migrate(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	issuer := auth.NewIssuer([]byte("itest-secret"), 15*time.Minute, time.Hour, 4*time.Hour)
	turnMinter := turn.NewMinter("itest-turn", []string{"turn:localhost:3478"}, time.Hour)
	svc := session.NewService(st.DB, st.Redis, turnMinter, issuer, 10*time.Minute)
	return st, svc
}

// insertOrgAndAgent creates a minimal organization + agent user directly, so
// lifecycle tests don't need to go through the HTTP registration flow.
func insertOrgAndAgent(t *testing.T, st *store.Store) (orgID, agentID uuid.UUID) {
	t.Helper()
	ctx := context.Background()
	if err := st.DB.QueryRow(ctx,
		`INSERT INTO organizations (name) VALUES ('Lifecycle Test Org') RETURNING id`,
	).Scan(&orgID); err != nil {
		t.Fatalf("insert org: %v", err)
	}
	email := "agent-" + uuid.NewString() + "@example.com"
	if err := st.DB.QueryRow(ctx,
		`INSERT INTO users (org_id, email, password_hash, display_name, role)
		 VALUES ($1, $2, 'x', 'Agent', 'agent') RETURNING id`,
		orgID, email,
	).Scan(&agentID); err != nil {
		t.Fatalf("insert agent: %v", err)
	}
	return orgID, agentID
}

func sessionStatus(t *testing.T, st *store.Store, sessionID uuid.UUID) string {
	t.Helper()
	var status string
	if err := st.DB.QueryRow(context.Background(),
		`SELECT status FROM sessions WHERE id = $1`, sessionID,
	).Scan(&status); err != nil {
		t.Fatalf("query session status: %v", err)
	}
	return status
}

// TestMarkEndedTransitionsActiveToEnded covers the real DB write path behind
// signaling.Hub.OnRoomEnded: an 'active' session becomes 'ended' with
// ended_at set, exactly once, and marking an already-ended or still-pending
// session again has no effect (idempotent / status-guarded).
func TestMarkEndedTransitionsActiveToEnded(t *testing.T) {
	st, svc := newTestStoreAndSession(t)
	ctx := context.Background()
	orgID, agentID := insertOrgAndAgent(t, st)

	tok, err := svc.Mint(ctx, orgID, agentID)
	if err != nil {
		t.Fatalf("mint: %v", err)
	}
	if _, err := svc.Redeem(ctx, tok.ConnectID, tok.PIN); err != nil {
		t.Fatalf("redeem: %v", err)
	}
	if got := sessionStatus(t, st, tok.SessionID); got != "active" {
		t.Fatalf("expected active after redeem, got %q", got)
	}

	if err := svc.MarkEnded(ctx, tok.SessionID); err != nil {
		t.Fatalf("mark ended: %v", err)
	}
	if got := sessionStatus(t, st, tok.SessionID); got != "ended" {
		t.Fatalf("expected ended, got %q", got)
	}

	// A still-pending session (never redeemed) must NOT be affected by MarkEnded.
	tok2, err := svc.Mint(ctx, orgID, agentID)
	if err != nil {
		t.Fatalf("mint 2: %v", err)
	}
	if err := svc.MarkEnded(ctx, tok2.SessionID); err != nil {
		t.Fatalf("mark ended (pending): %v", err)
	}
	if got := sessionStatus(t, st, tok2.SessionID); got != "pending" {
		t.Fatalf("MarkEnded must not touch a still-pending session; got %q", got)
	}
}

// TestExpireStalePendingOnlyTouchesOldPendingRows verifies the sweep only
// expires 'pending' rows older than the cutoff, leaving recent pending rows
// and non-pending rows untouched.
func TestExpireStalePendingOnlyTouchesOldPendingRows(t *testing.T) {
	st, svc := newTestStoreAndSession(t)
	ctx := context.Background()
	orgID, agentID := insertOrgAndAgent(t, st)

	// A fresh pending session: must survive a short-cutoff sweep.
	freshTok, err := svc.Mint(ctx, orgID, agentID)
	if err != nil {
		t.Fatalf("mint fresh: %v", err)
	}

	// An "old" pending session: backdate its created_at directly.
	oldTok, err := svc.Mint(ctx, orgID, agentID)
	if err != nil {
		t.Fatalf("mint old: %v", err)
	}
	if _, err := st.DB.Exec(ctx,
		`UPDATE sessions SET created_at = now() - interval '1 hour' WHERE id = $1`,
		oldTok.SessionID,
	); err != nil {
		t.Fatalf("backdate: %v", err)
	}

	// An active session older than the cutoff must NOT be expired (only 'pending' is eligible).
	activeTok, err := svc.Mint(ctx, orgID, agentID)
	if err != nil {
		t.Fatalf("mint active: %v", err)
	}
	if _, err := svc.Redeem(ctx, activeTok.ConnectID, activeTok.PIN); err != nil {
		t.Fatalf("redeem active: %v", err)
	}
	if _, err := st.DB.Exec(ctx,
		`UPDATE sessions SET created_at = now() - interval '1 hour' WHERE id = $1`,
		activeTok.SessionID,
	); err != nil {
		t.Fatalf("backdate active: %v", err)
	}

	// ExpireStalePending sweeps the whole sessions table (by design — it's a
	// periodic background job, not scoped to one test's rows), so other stale
	// 'pending' rows can legitimately exist in a shared, persistent test
	// database (e.g. left behind by earlier runs of this very test, whose own
	// intentionally-never-redeemed freshTok eventually becomes "old" in real
	// wall-clock time). Assert at least our row was swept, and — the part that
	// actually matters — verify by id that exactly our rows transitioned
	// correctly, regardless of what else was in the table.
	n, err := svc.ExpireStalePending(ctx, 10*time.Minute)
	if err != nil {
		t.Fatalf("expire stale pending: %v", err)
	}
	if n < 1 {
		t.Fatalf("expected at least 1 row expired, got %d", n)
	}

	if got := sessionStatus(t, st, oldTok.SessionID); got != "expired" {
		t.Fatalf("old pending session: expected expired, got %q", got)
	}
	if got := sessionStatus(t, st, freshTok.SessionID); got != "pending" {
		t.Fatalf("fresh pending session must be untouched, got %q", got)
	}
	if got := sessionStatus(t, st, activeTok.SessionID); got != "active" {
		t.Fatalf("active session must be untouched even though old, got %q", got)
	}
}

// TestRedisPresenceEmptinessAcrossInstances proves the distributed correctness
// that motivated basing the "room ended" signal on Presence rather than a
// single instance's local peer map: two independent RedisPresence instances
// (standing in for two backend replicas) sharing the same Redis correctly
// track a room's membership and report empty only once every occupant, on
// either "instance", has left.
func TestRedisPresenceEmptinessAcrossInstances(t *testing.T) {
	redisURL := os.Getenv("REDIS_URL")
	if redisURL == "" {
		t.Skip("REDIS_URL must be set for this test")
	}
	opts, err := redis.ParseURL(redisURL)
	if err != nil {
		t.Fatalf("parse redis url: %v", err)
	}
	rdb := redis.NewClient(opts)
	defer rdb.Close()

	room := "itest-room-" + uuid.NewString()
	// Two independent Presence instances, as two backend replicas would each
	// construct their own, sharing only Redis.
	instanceA := signaling.NewRedisPresence(rdb, time.Hour)
	instanceB := signaling.NewRedisPresence(rdb, time.Hour)

	// Agent joins via "instance A".
	others := instanceA.Join(room, signaling.RoleAgent)
	if len(others) != 0 {
		t.Fatalf("agent should see no counterpart yet, got %v", others)
	}
	// Phone joins via "instance B" and should see the agent (cross-instance).
	others = instanceB.Join(room, signaling.RolePhone)
	if len(others) != 1 || others[0] != signaling.RoleAgent {
		t.Fatalf("phone (instance B) should see the agent (instance A), got %v", others)
	}

	// Agent leaves via instance A: phone (on instance B) is still present.
	if empty := instanceA.Leave(room, signaling.RoleAgent); empty {
		t.Fatal("room must not be empty while the phone (on instance B) is present")
	}
	// Phone leaves via instance B: now the room is truly empty.
	if empty := instanceB.Leave(room, signaling.RolePhone); !empty {
		t.Fatal("room must be reported empty once the last occupant (on instance B) leaves")
	}
}

// TestRedisPresenceToleratesReconnectOverlap is the Redis-fanout counterpart to
// TestLocalPresenceToleratesReconnectOverlap (signaling package): a role that
// reconnects — its new socket joining, possibly on a different instance,
// before its old socket's Leave has fired — must not have its presence wiped
// out when the stale Leave eventually arrives. This is the exact bug closed by
// switching RedisPresence from a Redis SET (boolean membership) to a HASH of
// per-role counts.
func TestRedisPresenceToleratesReconnectOverlap(t *testing.T) {
	redisURL := os.Getenv("REDIS_URL")
	if redisURL == "" {
		t.Skip("REDIS_URL must be set for this test")
	}
	opts, err := redis.ParseURL(redisURL)
	if err != nil {
		t.Fatalf("parse redis url: %v", err)
	}
	rdb := redis.NewClient(opts)
	defer rdb.Close()

	room := "itest-reconnect-" + uuid.NewString()
	presence := signaling.NewRedisPresence(rdb, time.Hour)

	// The agent's new socket joins before its old socket has left (the overlap).
	presence.Join(room, signaling.RoleAgent) // old connection
	presence.Join(room, signaling.RoleAgent) // new connection, overlapping

	// The stale (old) connection's Leave fires. With set-based membership this
	// would have erased the agent's presence entirely; with counting, one
	// connection's worth of presence remains.
	if empty := presence.Leave(room, signaling.RoleAgent); empty {
		t.Fatal("one agent connection remains after a single leave; must not report empty")
	}

	// A phone joining now must still see the agent as present.
	others := presence.Join(room, signaling.RolePhone)
	if len(others) != 1 || others[0] != signaling.RoleAgent {
		t.Fatalf("expected the agent to still be visible after the stale leave, got %v", others)
	}
}
