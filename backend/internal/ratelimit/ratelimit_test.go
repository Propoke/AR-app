package ratelimit

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

// fakeStore is an in-memory Store for tests.
type fakeStore struct {
	mu       sync.Mutex
	counts   map[string]int64
	failIncr bool
}

func newFakeStore() *fakeStore { return &fakeStore{counts: map[string]int64{}} }

func (f *fakeStore) Incr(_ context.Context, key string) (int64, error) {
	if f.failIncr {
		return 0, errors.New("boom")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.counts[key]++
	return f.counts[key], nil
}

func (f *fakeStore) Expire(_ context.Context, _ string, _ time.Duration) error { return nil }

func TestAllowEnforcesLimit(t *testing.T) {
	l := New(newFakeStore(), 3, time.Minute, nil, TrustedProxies{})
	ctx := context.Background()

	for i := 1; i <= 3; i++ {
		if !l.Allow(ctx, "k") {
			t.Fatalf("request %d should be allowed", i)
		}
	}
	if l.Allow(ctx, "k") {
		t.Fatal("4th request should be blocked")
	}
	// A different key has its own budget.
	if !l.Allow(ctx, "other") {
		t.Fatal("a different key should be allowed")
	}
}

func TestAllowFailsOpenOnError(t *testing.T) {
	store := newFakeStore()
	store.failIncr = true
	l := New(store, 1, time.Minute, nil, TrustedProxies{})
	if !l.Allow(context.Background(), "k") {
		t.Fatal("limiter should fail open when the backend errors")
	}
}

func TestMiddlewareReturns429(t *testing.T) {
	l := New(newFakeStore(), 1, time.Minute, nil, TrustedProxies{})
	handler := l.Middleware("login", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	newReq := func() *http.Request {
		r := httptest.NewRequest(http.MethodPost, "/v1/auth/login", nil)
		r.RemoteAddr = "203.0.113.5:1234"
		return r
	}

	first := httptest.NewRecorder()
	handler(first, newReq())
	if first.Code != http.StatusOK {
		t.Fatalf("first request: want 200, got %d", first.Code)
	}

	second := httptest.NewRecorder()
	handler(second, newReq())
	if second.Code != http.StatusTooManyRequests {
		t.Fatalf("second request: want 429, got %d", second.Code)
	}
}

func TestClientIPIgnoresXFFFromUntrustedPeer(t *testing.T) {
	// No trusted proxies configured (the default/safe posture, e.g. the
	// WireGuard/LAN scenario with no reverse proxy in front of the backend).
	// Even though X-Forwarded-For is present, an untrusted peer must not be
	// able to claim a different IP through it.
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "10.0.0.1:5000"
	r.Header.Set("X-Forwarded-For", "198.51.100.7, 10.0.0.1")
	if ip := ClientIP(r, TrustedProxies{}); ip != "10.0.0.1" {
		t.Fatalf("untrusted peer's XFF must be ignored; want raw peer ip, got %q", ip)
	}
}

func TestClientIPHonorsXFFFromTrustedPeer(t *testing.T) {
	// The reverse proxy's own address (e.g. Caddy's) is trusted, so its XFF is
	// honored to recover the real client IP.
	trusted, err := ParseTrustedProxies("10.0.0.1/32")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "10.0.0.1:5000"
	r.Header.Set("X-Forwarded-For", "198.51.100.7, 10.0.0.1")
	if ip := ClientIP(r, trusted); ip != "198.51.100.7" {
		t.Fatalf("want forwarded client ip from trusted proxy, got %q", ip)
	}
}

func TestClientIPFallsBackWithoutXFF(t *testing.T) {
	trusted, _ := ParseTrustedProxies("10.0.0.1/32")
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "10.0.0.1:5000"
	// No XFF header at all, even from a trusted peer: use the raw address.
	if ip := ClientIP(r, trusted); ip != "10.0.0.1" {
		t.Fatalf("want raw peer ip when no XFF present, got %q", ip)
	}
}

func TestParseTrustedProxies(t *testing.T) {
	t.Run("empty input trusts nothing", func(t *testing.T) {
		tp, err := ParseTrustedProxies("")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if tp.trusts(mustParseIP(t, "127.0.0.1")) {
			t.Fatal("empty config must trust nothing")
		}
	})

	t.Run("multiple CIDRs, whitespace tolerant", func(t *testing.T) {
		tp, err := ParseTrustedProxies(" 172.20.0.0/16 , 10.0.0.1/32 ")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !tp.trusts(mustParseIP(t, "172.20.5.9")) {
			t.Fatal("expected 172.20.5.9 to be trusted (within 172.20.0.0/16)")
		}
		if !tp.trusts(mustParseIP(t, "10.0.0.1")) {
			t.Fatal("expected the exact /32 to be trusted")
		}
		if tp.trusts(mustParseIP(t, "8.8.8.8")) {
			t.Fatal("8.8.8.8 must not be trusted")
		}
	})

	t.Run("invalid CIDR errors", func(t *testing.T) {
		if _, err := ParseTrustedProxies("not-a-cidr"); err == nil {
			t.Fatal("expected an error for an invalid CIDR")
		}
	})
}

func mustParseIP(t *testing.T, s string) net.IP {
	t.Helper()
	ip := net.ParseIP(s)
	if ip == nil {
		t.Fatalf("invalid test IP %q", s)
	}
	return ip
}

// TestMiddlewareCannotBeBypassedBySpoofedXFF is the end-to-end regression test
// for the vulnerability: without a trusted-proxy configuration (e.g. the
// WireGuard/LAN deployment, where clients hit the backend directly), a client
// rotating its X-Forwarded-For header on every request must not be able to
// dodge its own rate limit — the limiter must still key on its real
// connection address.
func TestMiddlewareCannotBeBypassedBySpoofedXFF(t *testing.T) {
	l := New(newFakeStore(), 1, time.Minute, nil, TrustedProxies{}) // no trusted proxies
	handler := l.Middleware("login", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	spoofedReq := func(fakeIP string) *http.Request {
		r := httptest.NewRequest(http.MethodPost, "/v1/auth/login", nil)
		r.RemoteAddr = "203.0.113.5:1234" // the real, untrusted peer
		r.Header.Set("X-Forwarded-For", fakeIP)
		return r
	}

	first := httptest.NewRecorder()
	handler(first, spoofedReq("1.1.1.1"))
	if first.Code != http.StatusOK {
		t.Fatalf("first request: want 200, got %d", first.Code)
	}

	// A different spoofed XFF on each subsequent request must NOT grant a
	// fresh budget — the real peer address is still the same.
	second := httptest.NewRecorder()
	handler(second, spoofedReq("2.2.2.2"))
	if second.Code != http.StatusTooManyRequests {
		t.Fatalf("spoofed XFF must not bypass the limit; want 429, got %d", second.Code)
	}
}
