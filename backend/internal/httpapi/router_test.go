package httpapi

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"

	"github.com/propoke/ar-app/backend/internal/auth"
	"github.com/propoke/ar-app/backend/internal/signaling"
)

// TestSignalingUpgradeThroughMiddleware guards against a regression where the
// logging/metrics wrappers broke WebSocket upgrades by not forwarding Hijack.
// It needs no database: only the signaling handler and issuer are exercised.
func TestSignalingUpgradeThroughMiddleware(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	issuer := auth.NewIssuer([]byte("secret"), time.Minute, time.Hour, time.Hour)
	hub := signaling.NewHub()

	handler := New(Deps{
		Signaling: signaling.NewHandler(hub, logger, nil), // nil authorizer: no token needed
		Issuer:    issuer,
		Logger:    logger,
	})
	srv := httptest.NewServer(handler)
	defer srv.Close()

	room := uuid.NewString()
	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/v1/signaling?room=" + room + "&role=agent"
	conn, resp, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		status := 0
		if resp != nil {
			status = resp.StatusCode
		}
		t.Fatalf("signaling upgrade should succeed through middleware, got err=%v status=%d", err, status)
	}
	conn.Close()
}

// TestMetricsAndHealthEndpoints confirms the observability routes are served.
func TestMetricsAndHealthEndpoints(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	handler := New(Deps{
		Signaling: signaling.NewHandler(signaling.NewHub(), logger, nil),
		Issuer:    auth.NewIssuer([]byte("s"), time.Minute, time.Hour, time.Hour),
		Logger:    logger,
	})
	srv := httptest.NewServer(handler)
	defer srv.Close()

	for path, want := range map[string]int{"/healthz": 200, "/metrics": 200} {
		resp, err := http.Get(srv.URL + path)
		if err != nil {
			t.Fatalf("GET %s: %v", path, err)
		}
		resp.Body.Close()
		if resp.StatusCode != want {
			t.Fatalf("GET %s: want %d, got %d", path, want, resp.StatusCode)
		}
	}
}
