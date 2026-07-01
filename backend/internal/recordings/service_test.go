package recordings

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

// TestCompleteRejectsInvalidSizeBeforeTouchingDB confirms the size_bytes bound
// is checked before any database access, so it can be tested with a nil pool —
// a client-reported non-positive or absurdly large size must never even reach
// a write attempt.
func TestCompleteRejectsInvalidSizeBeforeTouchingDB(t *testing.T) {
	svc := NewService(nil, nil, time.Hour)
	for _, size := range []int64{0, -1, -1000, MaxRecordingSizeBytes + 1} {
		err := svc.Complete(context.Background(), uuid.New(), uuid.New(), size)
		if !errors.Is(err, ErrInvalidSize) {
			t.Fatalf("size %d: want ErrInvalidSize, got %v", size, err)
		}
	}
}
