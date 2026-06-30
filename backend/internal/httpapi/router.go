// Package httpapi wires the HTTP routes for all backend services.
package httpapi

import (
	"bufio"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/propoke/ar-app/backend/internal/auth"
	"github.com/propoke/ar-app/backend/internal/identity"
	"github.com/propoke/ar-app/backend/internal/metrics"
	"github.com/propoke/ar-app/backend/internal/ratelimit"
	"github.com/propoke/ar-app/backend/internal/recordings"
	"github.com/propoke/ar-app/backend/internal/session"
	"github.com/propoke/ar-app/backend/internal/signaling"
)

// Deps holds the constructed handlers and middleware the router needs.
type Deps struct {
	Identity  *identity.Handlers
	Session   *session.Handlers
	Signaling *signaling.Handler
	Issuer    *auth.Issuer
	Logger    *slog.Logger
	// Readiness reports 200 when dependencies (DB, Redis) are reachable, else 503.
	Readiness http.HandlerFunc
	// AuthLimiter and RedeemLimiter throttle brute-force attempts. Optional (nil
	// disables throttling for that route).
	AuthLimiter   *ratelimit.Limiter
	RedeemLimiter *ratelimit.Limiter
	// Recordings is optional; nil disables the recording endpoints.
	Recordings *recordings.Handlers
}

// New builds the top-level HTTP handler with all routes and global middleware.
func New(d Deps) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	// Readiness probe and Prometheus scrape endpoint.
	if d.Readiness != nil {
		mux.HandleFunc("GET /readyz", d.Readiness)
	}
	mux.Handle("GET /metrics", metrics.Handler())

	// Public auth endpoints. Login is throttled per client IP.
	mux.HandleFunc("POST /v1/auth/register", d.Identity.Register)
	mux.HandleFunc("POST /v1/auth/login", throttle(d.AuthLimiter, "login", d.Identity.Login))
	mux.HandleFunc("POST /v1/auth/refresh", d.Identity.Refresh)

	// Public token redeem (the phone presents connect id + PIN; the token is the
	// credential, so no bearer auth here). Throttled per client IP.
	mux.HandleFunc("POST /v1/sessions/redeem", throttle(d.RedeemLimiter, "redeem", d.Session.Redeem))

	// Signaling WebSocket (room id is the bearer in Phase 1; see handler note).
	mux.HandleFunc("GET /v1/signaling", d.Signaling.ServeWS)

	// Authenticated endpoints (per-route, so {id} path values are available).
	authed := func(h http.HandlerFunc) http.Handler {
		return d.Issuer.Middleware(http.HandlerFunc(h))
	}
	mux.Handle("GET /v1/me", authed(d.Identity.Me))
	mux.Handle("POST /v1/sessions", authed(d.Session.Mint))

	// Session recordings (optional; only when object storage is configured).
	if d.Recordings != nil {
		mux.Handle("POST /v1/sessions/{id}/recordings", authed(d.Recordings.CreateUpload))
		mux.Handle("GET /v1/sessions/{id}/recordings", authed(d.Recordings.List))
		mux.Handle("POST /v1/recordings/{id}/complete", authed(d.Recordings.Complete))
	}

	return instrument(logging(d.Logger, recoverer(d.Logger, mux)))
}

// throttle wraps a handler with the limiter if one is configured.
func throttle(l *ratelimit.Limiter, prefix string, h http.HandlerFunc) http.HandlerFunc {
	if l == nil {
		return h
	}
	return l.Middleware(prefix, h)
}

// instrument records request metrics. Paths in this API are low-cardinality
// (no ids are embedded in the path), so the raw path is a safe metric label.
func instrument(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		metrics.Middleware(r.URL.Path, next.ServeHTTP)(w, r)
	})
}

// logging emits one structured line per request with method, path, status, and latency.
func logging(log *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(sw, r)
		log.Info("http",
			"method", r.Method,
			"path", r.URL.Path,
			"status", sw.status,
			"dur_ms", time.Since(start).Milliseconds(),
		)
	})
}

// recoverer converts a panic in a handler into a 500 instead of crashing the server.
func recoverer(log *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				log.Error("panic", "err", rec, "path", r.URL.Path)
				http.Error(w, "internal error", http.StatusInternalServerError)
			}
		}()
		next.ServeHTTP(w, r)
	})
}

type statusWriter struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
}

func (w *statusWriter) WriteHeader(code int) {
	if !w.wroteHeader {
		w.status = code
		w.wroteHeader = true
	}
	w.ResponseWriter.WriteHeader(code)
}

// Hijack lets the WebSocket upgrader take over the connection through the logging
// wrapper. It delegates to the underlying ResponseWriter's Hijacker.
func (w *statusWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	hj, ok := w.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, errors.New("underlying ResponseWriter does not support hijacking")
	}
	return hj.Hijack()
}
