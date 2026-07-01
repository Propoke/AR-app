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
	"github.com/propoke/ar-app/backend/internal/metrics"
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

// TestInstrumentCollapsesWildcardCardinalityAndPreservesPathValues guards two
// things at once for a wildcard route like "/widgets/{id}/touch":
//
//  1. Prometheus label cardinality: hitting the same route with many distinct
//     ids must record ONE time series keyed by the route *pattern*, not one
//     series per raw path/id (which would grow unbounded — exactly the bug
//     that existed when instrument() labeled by r.URL.Path).
//  2. Routing correctness: the actual handler must still see the correct
//     r.PathValue("id") for each request. This is the regression risk of the
//     naive fix (resolving the pattern via mux.Handler(r) up front, which does
//     NOT populate the wildcard bindings ServeHTTP sets up during its own
//     dispatch) — that approach would silently return "" from PathValue for
//     every {id} route in this app (user disable/enable, recordings).
func TestInstrumentCollapsesWildcardCardinalityAndPreservesPathValues(t *testing.T) {
	mux := http.NewServeMux()
	var seenIDs []string
	mux.HandleFunc("POST /widgets/{id}/touch", func(w http.ResponseWriter, r *http.Request) {
		seenIDs = append(seenIDs, r.PathValue("id"))
		w.WriteHeader(http.StatusOK)
	})

	srv := httptest.NewServer(instrument(mux))
	defer srv.Close()

	ids := []string{uuid.NewString(), uuid.NewString(), uuid.NewString()}
	for _, id := range ids {
		resp, err := http.Post(srv.URL+"/widgets/"+id+"/touch", "application/json", nil)
		if err != nil {
			t.Fatalf("post: %v", err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("want 200, got %d", resp.StatusCode)
		}
	}

	// (2) The handler must have resolved the correct wildcard value each time.
	if len(seenIDs) != len(ids) {
		t.Fatalf("expected %d handler invocations, got %d", len(ids), len(seenIDs))
	}
	for i, id := range ids {
		if seenIDs[i] != id {
			t.Fatalf("PathValue mismatch at index %d: want %q got %q", i, id, seenIDs[i])
		}
	}

	// (1) Scrape /metrics: exactly one series for the pattern, and none of the
	// raw ids leaked into a label anywhere in the output.
	scrape := httptest.NewRecorder()
	metrics.Handler().ServeHTTP(scrape, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	body := scrape.Body.String()

	wantPrefix := `ar_http_requests_total{method="POST",route="POST /widgets/{id}/touch",status="200"} `
	if n := strings.Count(body, wantPrefix); n != 1 {
		t.Fatalf("expected exactly 1 collapsed series for the wildcard route, found %d; body:\n%s", n, body)
	}
	for _, id := range ids {
		if strings.Contains(body, id) {
			t.Fatalf("a raw path id (%s) leaked into the metrics output", id)
		}
	}
}

// TestRoutePatternFallsBackWhenUnmatched covers the 404/no-match case, where
// ServeMux leaves r.Pattern empty.
func TestRoutePatternFallsBackWhenUnmatched(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/does-not-exist", nil)
	if got := routePattern(req); got != "unmatched" {
		t.Fatalf("want %q, got %q", "unmatched", got)
	}
}
