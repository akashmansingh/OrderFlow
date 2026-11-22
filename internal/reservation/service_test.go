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

func TestCreateReservationFastPathDisabledByFlag(t *testing.T) {
	cache := newStubCache()
	eventID := "11111111-1111-1111-1111-111111111111"
	cache.soldOutEvents[eventID] = true

	// With SoldOutFastPath disabled, fast-path check is bypassed, proceeding to DB.
	// Since databasePool is nil, it will panic on Begin rather than immediately returning ErrInsufficientInventory.
	svc := reservation.NewService(nil, 10*time.Minute).
		WithCache(cache, 5*time.Second).
		WithFeatureFlags(true, false)

	input := reservation.CreateReservationInput{
		EventID:        eventID,
		CustomerID:     "22222222-2222-2222-2222-222222222222",
		Quantity:       1,
		IdempotencyKey: "idemp-test",
		RequestHash:    []byte("hash"),
	}

	defer func() {
		if r := recover(); r == nil {
			t.Fatalf("expected nil database pool panic when fast path is disabled, but call succeeded or failed without DB call")
		}
	}()

	_, _ = svc.CreateReservation(context.Background(), input)
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

func TestSingleflightContextIsolation(t *testing.T) {
	cache := newStubCache()
	eventID := "11111111-1111-1111-1111-111111111111"
	cache.soldOutEvents[eventID] = true

	slowCache := &slowStubCache{
		underlying: cache,
		onCheck: func() {
			time.Sleep(50 * time.Millisecond)
		},
	}

	svc := reservation.NewService(nil, 10*time.Minute).WithCache(slowCache, 5*time.Second)

	// Caller 1 has a very short context that will cancel while the singleflight operation is running
	ctx1, cancel1 := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel1()

	// Caller 2 has an ample context
	ctx2 := context.Background()

	input1 := reservation.CreateReservationInput{
		EventID:        eventID,
		CustomerID:     "22222222-2222-2222-2222-222222222222",
		Quantity:       1,
		IdempotencyKey: "idemp-cancel-1",
		RequestHash:    []byte("hash"),
	}

	input2 := reservation.CreateReservationInput{
		EventID:        eventID,
		CustomerID:     "33333333-3333-3333-3333-333333333333",
		Quantity:       1,
		IdempotencyKey: "idemp-cancel-2",
		RequestHash:    []byte("hash"),
	}

	var wg sync.WaitGroup
	var err2 error
	wg.Add(2)

	go func() {
		defer wg.Done()
		_, _ = svc.CreateReservation(ctx1, input1)
	}()

	go func() {
		defer wg.Done()
		// Small delay to ensure caller 1 initiates the singleflight Do call
		time.Sleep(5 * time.Millisecond)
		_, err2 = svc.CreateReservation(ctx2, input2)
	}()

	wg.Wait()

	// Caller 2 must still get ErrInsufficientInventory (cached sold out) rather than being corrupted by caller 1's cancellation
	if !errors.Is(err2, reservation.ErrInsufficientInventory) {
		t.Fatalf("caller 2 error = %v, want ErrInsufficientInventory", err2)
	}
}
