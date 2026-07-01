package signaling

import (
	"sync"
	"testing"
)

// TestSendNeverRacesCloseSend hammers Send and closeSend concurrently under
// go test -race. Before the mutex guard, a Send losing the race to closeSend
// would panic on "send on closed channel" — a bug that would crash the entire
// backend process (every active session) when a peer disconnected mid-relay.
// This asserts no panic and no data race across many attempts.
func TestSendNeverRacesCloseSend(t *testing.T) {
	for i := 0; i < 200; i++ {
		p := &wsPeer{id: "x", role: RoleAgent, send: make(chan []byte, sendBuffer)}

		var wg sync.WaitGroup
		wg.Add(2)

		// Drain so buffered sends don't block.
		done := make(chan struct{})
		go func() {
			for range p.send {
			}
			close(done)
		}()

		go func() {
			defer wg.Done()
			for j := 0; j < 20; j++ {
				p.Send([]byte("frame"))
			}
		}()
		go func() {
			defer wg.Done()
			p.closeSend()
		}()

		wg.Wait()
		<-done // channel fully drained/closed, drainer goroutine exited

		// Sending after close must be a safe no-op, never a panic.
		if p.Send([]byte("late")) {
			t.Fatal("Send after closeSend should return false")
		}
	}
}

// TestCloseSendIsIdempotent ensures calling closeSend twice never double-closes
// the channel (which would itself panic).
func TestCloseSendIsIdempotent(t *testing.T) {
	p := &wsPeer{id: "x", role: RolePhone, send: make(chan []byte, sendBuffer)}
	p.closeSend()
	p.closeSend() // must not panic
}
