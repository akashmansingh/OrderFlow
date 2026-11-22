package outbox_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/theRiddler9/OrderFlow/internal/events"
	"github.com/theRiddler9/OrderFlow/internal/outbox"
)

func TestRelayRunOnceLeavesEventUnpublishedAfterPublishFailure(t *testing.T) {
	store := &memoryStore{next: heldEvent()}
	relay := outbox.NewRelay(store, failingPublisher{err: errors.New("broker unavailable")}, time.Second)

	err := relay.RunOnce(context.Background())
	if err == nil {
		t.Fatal("RunOnce() error = nil, want publish failure")
	}
	if store.markedPublished != 0 {
		t.Fatalf("markedPublished = %d, want 0", store.markedPublished)
	}
	if store.publishFailures != 1 {
		t.Fatalf("publishFailures = %d, want 1", store.publishFailures)
	}
}

func TestRelayRunOnceMarksEventPublishedAfterBrokerAcknowledgement(t *testing.T) {
	store := &memoryStore{next: heldEvent()}
	relay := outbox.NewRelay(store, succeedingPublisher{}, time.Second)

	if err := relay.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce() error = %v", err)
	}
	if store.markedPublished != 1 {
		t.Fatalf("markedPublished = %d, want 1", store.markedPublished)
	}
	if store.publishFailures != 0 {
		t.Fatalf("publishFailures = %d, want 0", store.publishFailures)
	}
}

type memoryStore struct {
	next            events.Envelope
	claimed         bool
	markedPublished int
	publishFailures int
}

func (store *memoryStore) ReportBacklog(context.Context) (int64, float64, error) {
	if store.claimed {
		return 0, 0, nil
	}
	return 1, 0.5, nil
}

func (store *memoryStore) ClaimNext(context.Context) (events.Envelope, bool, error) {
	if store.claimed {
		return events.Envelope{}, false, nil
	}
	store.claimed = true
	return store.next, true, nil
}

func (store *memoryStore) MarkPublished(context.Context, string) error {
	store.markedPublished++
	return nil
}

func (store *memoryStore) RecordPublishFailure(context.Context, string) error {
	store.publishFailures++
	return nil
}

type failingPublisher struct{ err error }

func (publisher failingPublisher) Publish(context.Context, events.Envelope) error {
	return publisher.err
}

type succeedingPublisher struct{}

func (succeedingPublisher) Publish(context.Context, events.Envelope) error {
	return nil
}

func heldEvent() events.Envelope {
	return events.Envelope{
		ID:          "d2719c30-1edf-4c5f-9273-cb1f55a3797e",
		Type:        "reservation.held.v1",
		AggregateID: "96e74b3e-01aa-469e-802d-f04e1dcfbdff",
		OccurredAt:  time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC),
		Payload:     []byte(`{"reservation_id":"61c26b50-c02a-4a86-a46d-1f2ea1078b9f"}`),
	}
}
