//go:build integration

// Package integration exercises the DB-backed HTTP flows against real Postgres and
// Redis. It is excluded from the normal unit suite (build tag "integration") and
// runs in CI with service containers. Run locally with:
//
//	DATABASE_URL=... REDIS_URL=... go test -tags=integration ./internal/integration/
package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/propoke/ar-app/backend/internal/auth"
	"github.com/propoke/ar-app/backend/internal/httpapi"
	"github.com/propoke/ar-app/backend/internal/identity"
	"github.com/propoke/ar-app/backend/internal/session"
	"github.com/propoke/ar-app/backend/internal/signaling"
	"github.com/propoke/ar-app/backend/internal/store"
	"github.com/propoke/ar-app/backend/internal/turn"
	"log/slog"
)

func newServer(t *testing.T) *httptest.Server {
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
	hub := signaling.NewHub()
	signalingAuth := func(room, role, token string) error {
		claims, err := issuer.ParseSignaling(token)
		if err != nil {
			return err
		}
		if claims.Room != room || claims.Role != role {
			return errInvalid
		}
		return nil
	}

	handler := httpapi.New(httpapi.Deps{
		Identity:  identity.NewHandlers(identity.NewService(st.DB), issuer),
		Session:   session.NewHandlers(session.NewService(st.DB, st.Redis, turnMinter, issuer, 10*time.Minute)),
		Signaling: signaling.NewHandler(hub, slog.Default(), signalingAuth),
		Issuer:    issuer,
		Logger:    slog.Default(),
	})
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return srv
}

var errInvalid = &stringError{"token does not match room/role"}

type stringError struct{ s string }

func (e *stringError) Error() string { return e.s }

func postJSON(t *testing.T, url, token string, body any) (*http.Response, map[string]any) {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		_ = json.NewEncoder(&buf).Encode(body)
	}
	req, _ := http.NewRequest(http.MethodPost, url, &buf)
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request %s: %v", url, err)
	}
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	resp.Body.Close()
	return resp, out
}

// TestFullSessionFlow covers the DB-backed path end to end: register an org, mint
// a connection token, redeem it, and confirm both peers can join signaling with
// their tokens.
func TestFullSessionFlow(t *testing.T) {
	srv := newServer(t)

	// Register a unique org/admin.
	email := "tech-" + time.Now().Format("150405.000000") + "@example.com"
	resp, reg := postJSON(t, srv.URL+"/v1/auth/register", "", map[string]string{
		"org_name": "Acme", "email": email, "password": "hunter2hunter2", "display_name": "Tech",
	})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("register: want 201, got %d", resp.StatusCode)
	}
	access, _ := reg["access_token"].(string)
	if access == "" {
		t.Fatal("no access token returned")
	}

	// Mint a connection token.
	resp, mint := postJSON(t, srv.URL+"/v1/sessions", access, nil)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("mint: want 201, got %d", resp.StatusCode)
	}
	connectID, _ := mint["connect_id"].(string)
	pin, _ := mint["pin"].(string)
	room, _ := mint["room"].(string)
	agentToken, _ := mint["signaling_token"].(string)
	if connectID == "" || pin == "" || agentToken == "" {
		t.Fatalf("incomplete mint response: %v", mint)
	}

	// Redeem it from the "phone".
	resp, redeem := postJSON(t, srv.URL+"/v1/sessions/redeem", "", map[string]string{
		"connect_id": connectID, "pin": pin,
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("redeem: want 200, got %d", resp.StatusCode)
	}
	phoneToken, _ := redeem["signaling_token"].(string)
	if phoneToken == "" {
		t.Fatal("redeem returned no signaling token")
	}

	// A second redeem must fail (single-use).
	resp, _ = postJSON(t, srv.URL+"/v1/sessions/redeem", "", map[string]string{
		"connect_id": connectID, "pin": pin,
	})
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("second redeem: want 401, got %d", resp.StatusCode)
	}

	// Both peers can open signaling with their tokens.
	dialSignaling(t, srv.URL, room, "agent", agentToken, true)
	dialSignaling(t, srv.URL, room, "phone", phoneToken, true)
	// A bad token is rejected.
	dialSignaling(t, srv.URL, room, "agent", "garbage", false)
}

func dialSignaling(t *testing.T, baseURL, room, role, token string, wantOK bool) {
	t.Helper()
	wsURL := "ws" + strings.TrimPrefix(baseURL, "http") +
		"/v1/signaling?room=" + room + "&role=" + role + "&token=" + token
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if wantOK {
		if err != nil {
			t.Fatalf("%s should connect: %v", role, err)
		}
		conn.Close()
	} else if err == nil {
		conn.Close()
		t.Fatalf("%s with bad token should be rejected", role)
	}
}
