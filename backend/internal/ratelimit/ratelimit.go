// Package ratelimit provides a small fixed-window rate limiter and HTTP
// middleware, used to throttle abusive login and token-redeem attempts.
package ratelimit

import (
	"context"
	"fmt"
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

// TrustedProxies is a set of CIDR ranges whose immediate TCP connections are
// trusted to set an accurate X-Forwarded-For header (e.g. a co-located reverse
// proxy like Caddy). The zero value trusts nothing: X-Forwarded-For is always
// ignored and the raw connection address is used instead.
//
// This must default to "trust nothing", not "trust private IP ranges": in the
// WireGuard/LAN deployment scenario clients connect to the backend directly
// (no reverse proxy at all) over private tunnel/LAN addresses, so treating
// private ranges as trusted would let any client on the tunnel spoof
// X-Forwarded-For to a different IP and evade its own throttling — the exact
// vulnerability this type exists to close.
type TrustedProxies struct {
	nets []*net.IPNet
}

// ParseTrustedProxies parses a comma-separated list of CIDR ranges (e.g. from
// a TRUSTED_PROXY_CIDRS environment variable). Empty or whitespace-only input
// trusts nothing. Only set this to the reverse proxy's own address/subnet
// (e.g. the Docker network Caddy runs on) — never to a range that includes
// addresses clients might connect from directly.
func ParseTrustedProxies(cidrs string) (TrustedProxies, error) {
	var tp TrustedProxies
	for _, s := range strings.Split(cidrs, ",") {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		_, ipnet, err := net.ParseCIDR(s)
		if err != nil {
			return TrustedProxies{}, fmt.Errorf("invalid trusted proxy CIDR %q: %w", s, err)
		}
		tp.nets = append(tp.nets, ipnet)
	}
	return tp, nil
}

func (tp TrustedProxies) trusts(ip net.IP) bool {
	for _, n := range tp.nets {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

// Limiter allows up to Max events per Window for a given key.
type Limiter struct {
	store   Store
	max     int64
	window  time.Duration
	log     *slog.Logger
	trusted TrustedProxies
}

// New constructs a Limiter. trusted controls when X-Forwarded-For is honored
// when deriving the client IP to key on (see TrustedProxies); pass the zero
// value to always use the raw connection address.
func New(store Store, max int64, window time.Duration, log *slog.Logger, trusted TrustedProxies) *Limiter {
	return &Limiter{store: store, max: max, window: window, log: log, trusted: trusted}
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
		key := prefix + ":" + ClientIP(r, l.trusted)
		if !l.Allow(r.Context(), key) {
			w.Header().Set("Retry-After", "60")
			http.Error(w, "too many requests", http.StatusTooManyRequests)
			return
		}
		next(w, r)
	}
}

// ClientIP returns the best-effort client IP for rate-limiting purposes.
// X-Forwarded-For is honored only when the immediate connection (RemoteAddr)
// is in trusted; otherwise — including when the header is present but the
// peer is not trusted — it is ignored and the raw connection address is used.
// Trusting X-Forwarded-For from an arbitrary/untrusted peer would let that
// peer set it to any value and dodge its own rate limit entirely.
func ClientIP(r *http.Request, trusted TrustedProxies) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	if ip := net.ParseIP(host); ip != nil && trusted.trusts(ip) {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			if first := strings.TrimSpace(strings.Split(xff, ",")[0]); first != "" {
				return first
			}
		}
	}
	return host
}
