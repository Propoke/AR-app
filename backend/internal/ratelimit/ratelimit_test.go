package ratelimit

import (
	"context"
	"errors"
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
	l := New(newFakeStore(), 3, time.Minute, nil)
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
	l := New(store, 1, time.Minute, nil)
	if !l.Allow(context.Background(), "k") {
		t.Fatal("limiter should fail open when the backend errors")
	}
}

func TestMiddlewareReturns429(t *testing.T) {
	l := New(newFakeStore(), 1, time.Minute, nil)
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

func TestClientIPPrefersXFF(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "10.0.0.1:5000"
	r.Header.Set("X-Forwarded-For", "198.51.100.7, 10.0.0.1")
	if ip := ClientIP(r); ip != "198.51.100.7" {
		t.Fatalf("want forwarded client ip, got %q", ip)
	}
}
