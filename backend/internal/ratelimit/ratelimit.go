// Package ratelimit provides a small fixed-window rate limiter and HTTP
// middleware, used to throttle abusive login and token-redeem attempts.
package ratelimit

import (
	"context"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"
)

// Store is the minimal counter backend a Limiter needs. The Redis adapter
// (NewRedisStore) implements it; tests use a fake.
type Store interface {
	Incr(ctx context.Context, key string) (int64, error)
	Expire(ctx context.Context, key string, ttl time.Duration) error
}

// Limiter allows up to Max events per Window for a given key.
type Limiter struct {
	store  Store
	max    int64
	window time.Duration
	log    *slog.Logger
}

// New constructs a Limiter.
func New(store Store, max int64, window time.Duration, log *slog.Logger) *Limiter {
	return &Limiter{store: store, max: max, window: window, log: log}
}

// Allow reports whether an event for key is within the limit, incrementing the
// window counter. It fails open (allows) on backend errors so an outage cannot
// lock everyone out.
func (l *Limiter) Allow(ctx context.Context, key string) bool {
	n, err := l.store.Incr(ctx, key)
	if err != nil {
		if l.log != nil {
			l.log.Warn("ratelimit backend error; failing open", "err", err)
		}
		return true
	}
	if n == 1 {
		_ = l.store.Expire(ctx, key, l.window)
	}
	return n <= l.max
}

// Middleware throttles requests, keying by the prefix plus the client IP. Over
// the limit responds 429.
func (l *Limiter) Middleware(prefix string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		key := prefix + ":" + ClientIP(r)
		if !l.Allow(r.Context(), key) {
			w.Header().Set("Retry-After", "60")
			http.Error(w, "too many requests", http.StatusTooManyRequests)
			return
		}
		next(w, r)
	}
}

// ClientIP returns the best-effort client IP, honouring X-Forwarded-For set by a
// trusted reverse proxy (the first hop), falling back to RemoteAddr.
func ClientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		if first := strings.TrimSpace(strings.Split(xff, ",")[0]); first != "" {
			return first
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
