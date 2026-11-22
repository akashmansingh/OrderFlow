package reservation_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/theRiddler9/OrderFlow/internal/reservation"
)

type stubCache struct {
	soldOutEvents map[string]bool
	markedEvents  map[string]time.Duration
	clearedEvents []string
}

func newStubCache() *stubCache {
	return &stubCache{
		soldOutEvents: make(map[string]bool),
		markedEvents:  make(map[string]time.Duration),
	}
}

func (s *stubCache) IsSoldOut(ctx context.Context, eventID string) (bool, error) {
	return s.soldOutEvents[eventID], nil
}

func (s *stubCache) MarkSoldOut(ctx context.Context, eventID string, ttl time.Duration) error {
	s.markedEvents[eventID] = ttl
	s.soldOutEvents[eventID] = true
	return nil
}

func (s *stubCache) ClearSoldOut(ctx context.Context, eventID string) error {
	delete(s.soldOutEvents, eventID)
	s.clearedEvents = append(s.clearedEvents, eventID)
	return nil
}

type slowStubCache struct {
	underlying *stubCache
	onCheck    func()
}

func (s *slowStubCache) IsSoldOut(ctx context.Context, eventID string) (bool, error) {
	if s.onCheck != nil {
		s.onCheck()
	}
	return s.underlying.IsSoldOut(ctx, eventID)
}

func (s *slowStubCache) MarkSoldOut(ctx context.Context, eventID string, ttl time.Duration) error {
	return s.underlying.MarkSoldOut(ctx, eventID, ttl)
}

func (s *slowStubCache) ClearSoldOut(ctx context.Context, eventID string) error {
	return s.underlying.ClearSoldOut(ctx, eventID)
}

func TestCreateReservationFastPathRejectsSoldOutEvent(t *testing.T) {
	cache := newStubCache()
	eventID := "11111111-1111-1111-1111-111111111111"
	cache.soldOutEvents[eventID] = true

	// Note databasePool is nil: fast-path must not touch database
	svc := reservation.NewService(nil, 10*time.Minute).WithCache(cache, 5*time.Second)

	input := reservation.CreateReservationInput{
		EventID:        eventID,
		CustomerID:     "22222222-2222-2222-2222-222222222222",
		Quantity:       1,
		IdempotencyKey: "idemp-test",
		RequestHash:    []byte("hash"),
	}

	_, err := svc.CreateReservation(context.Background(), input)
	if !errors.Is(err, reservation.ErrInsufficientInventory) {
		t.Fatalf("expected ErrInsufficientInventory on cached sold out event, got %v", err)
	}
}

func TestSingleflightDeduplicatesConcurrentSoldOutChecks(t *testing.T) {
	var checkCount atomic.Int64
	cache := newStubCache()
	eventID := "11111111-1111-1111-1111-111111111111"
	cache.soldOutEvents[eventID] = true

	slowCache := &slowStubCache{
		underlying: cache,
		onCheck: func() {
			checkCount.Add(1)
			time.Sleep(25 * time.Millisecond)
		},
	}

	svc := reservation.NewService(nil, 10*time.Minute).WithCache(slowCache, 5*time.Second)

	var wg sync.WaitGroup
	concurrentRequests := 20
	errs := make(chan error, concurrentRequests)

	for i := 0; i < concurrentRequests; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			input := reservation.CreateReservationInput{
				EventID:        eventID,
				CustomerID:     "22222222-2222-2222-2222-222222222222",
				Quantity:       1,
				IdempotencyKey: fmt.Sprintf("idemp-%d", idx),
				RequestHash:    []byte("hash"),
			}
			_, err := svc.CreateReservation(context.Background(), input)
			errs <- err
		}(i)
	}

	wg.Wait()
	close(errs)

	for err := range errs {
		if !errors.Is(err, reservation.ErrInsufficientInventory) {
			t.Fatalf("expected ErrInsufficientInventory, got %v", err)
		}
	}

	count := checkCount.Load()
	if count > 2 {
		t.Fatalf("singleflight failed to deduplicate: checkCount = %d, want <= 2", count)
	}
}
