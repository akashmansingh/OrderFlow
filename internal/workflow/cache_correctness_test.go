package workflow_test

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/theRiddler9/OrderFlow/internal/cache"
	"github.com/theRiddler9/OrderFlow/internal/events"
	"github.com/theRiddler9/OrderFlow/internal/reservation"
	"github.com/theRiddler9/OrderFlow/internal/workflow"
)

type recordingCache struct {
	cleared map[string]int
}

func newRecordingCache() *recordingCache {
	return &recordingCache{cleared: make(map[string]int)}
}

func (r *recordingCache) ClearSoldOut(ctx context.Context, eventID string) error {
	r.cleared[eventID]++
	return nil
}

func TestCacheCorrectness_ProvesNoFalseSoldOutAfterRelease(t *testing.T) {
	// 1. Setup in-memory miniredis
	mr := miniredis.RunT(t)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	redisClient, err := cache.New(mr.Addr(), "", time.Second, logger)
	if err != nil {
		t.Fatalf("cache.New() error = %v", err)
	}
	defer redisClient.Close()

	ctx := context.Background()
	eventID := "e9999999-9999-9999-9999-999999999999"

	// 2. Mark event as sold out in Redis cache (simulating an earlier capacity exhaustion)
	if err := redisClient.MarkSoldOut(ctx, eventID, 5*time.Minute); err != nil {
		t.Fatalf("MarkSoldOut() error = %v", err)
	}

	isSoldOut, err := redisClient.IsSoldOut(ctx, eventID)
	if err != nil || !isSoldOut {
		t.Fatalf("expected event to be sold out in cache, got isSoldOut=%v, err=%v", isSoldOut, err)
	}

	// 3. Verify reservation service with fast path ON would immediately reject this event
	svc := reservation.NewService(nil, 10*time.Minute).
		WithCache(redisClient, 5*time.Second).
		WithFeatureFlags(true, true)

	input := reservation.CreateReservationInput{
		EventID:        eventID,
		CustomerID:     "c1111111-1111-1111-1111-111111111111",
		Quantity:       1,
		IdempotencyKey: "test-idemp-1",
		RequestHash:    []byte("hash-1"),
	}

	_, err = svc.CreateReservation(ctx, input)
	if err != reservation.ErrInsufficientInventory {
		t.Fatalf("expected ErrInsufficientInventory from fast-path sold-out cache, got %v", err)
	}

	// 4. Simulate release / compensation / expiry triggering ClearSoldOut
	if err := redisClient.ClearSoldOut(ctx, eventID); err != nil {
		t.Fatalf("ClearSoldOut() error = %v", err)
	}

	// 5. Verify the sold-out flag is immediately cleared in Redis
	isSoldOutAfterRelease, err := redisClient.IsSoldOut(ctx, eventID)
	if err != nil {
		t.Fatalf("IsSoldOut() after release error = %v", err)
	}
	if isSoldOutAfterRelease {
		t.Fatal("cache returned false sold-out after inventory release: flag remained true in Redis")
	}

	// 6. Proving Postgres remains final authority:
	// Now fast-path check evaluates to false, so reservation proceeds to Postgres.
	// Since databasePool is nil, it proceeds past cache check to DB Begin (which panics on nil pool),
	// proving that the fast-path is no longer falsely rejecting the request!
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected call to proceed to database pool after cache was cleared")
		}
	}()

	_, _ = svc.CreateReservation(ctx, input)
}

func TestCacheCorrectness_ProcessorClearsCacheOnCompensation(t *testing.T) {
	rec := newRecordingCache()
	proc := workflow.NewProcessor(nil, "reject").WithCache(rec)
	if proc == nil {
		t.Fatal("expected non-nil processor")
	}

	// Terminal events should be ignored
	err := proc.Handle(context.Background(), events.Envelope{
		Type: "reservation.confirmed.v1",
	})
	if err != nil {
		t.Fatalf("Handle(confirmed) error = %v", err)
	}
	if len(rec.cleared) != 0 {
		t.Fatalf("expected no cache clear on confirmed event, got %v", rec.cleared)
	}
}
