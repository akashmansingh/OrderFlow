package reservation_test

import (
	"context"
	"errors"
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
