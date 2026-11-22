package workflow_test

import (
	"context"
	"testing"
	"time"

	"github.com/theRiddler9/OrderFlow/internal/events"
	"github.com/theRiddler9/OrderFlow/internal/workflow"
)

func TestParseHeldReservationPayload(t *testing.T) {
	event := events.Envelope{
		Type: "reservation.held.v1",
		Payload: []byte(`{
			"reservation_id":"61c26b50-c02a-4a86-a46d-1f2ea1078b9f",
			"event_id":"96e74b3e-01aa-469e-802d-f04e1dcfbdff",
			"customer_id":"8a9db6db-01eb-4fd3-aeba-dd1255a4f970",
			"quantity":2,
			"expires_at":"2026-11-17T12:10:00Z"
		}`),
	}

	payload, err := workflow.ParseHeldReservation(event)
	if err != nil {
		t.Fatalf("ParseHeldReservation() error = %v", err)
	}
	if payload.Quantity != 2 {
		t.Fatalf("Quantity = %d, want 2", payload.Quantity)
	}
	if !payload.ExpiresAt.Equal(time.Date(2026, 11, 17, 12, 10, 0, 0, time.UTC)) {
		t.Fatalf("ExpiresAt = %s", payload.ExpiresAt)
	}
}

func TestProcessorAcknowledgesTerminalEventWithoutDatabaseWork(t *testing.T) {
	processor := workflow.NewProcessor(nil, "approve")
	terminalTypes := []string{
		"reservation.confirmed.v1",
		"reservation.cancelled.v1",
		"reservation.expired.v1",
	}

	for _, eventType := range terminalTypes {
		err := processor.Handle(context.Background(), events.Envelope{Type: eventType})
		if err != nil {
			t.Fatalf("Handle(%s) error = %v, want nil for terminal event", eventType, err)
		}
	}
}

func TestParseHeldReservationRejectsMissingQuantity(t *testing.T) {
	_, err := workflow.ParseHeldReservation(events.Envelope{
		Type:    "reservation.held.v1",
		Payload: []byte(`{"reservation_id":"61c26b50-c02a-4a86-a46d-1f2ea1078b9f"}`),
	})
	if err == nil {
		t.Fatal("ParseHeldReservation() error = nil, want validation error")
	}
}

type stubWorkflowCache struct {
	cleared []string
}

func (s *stubWorkflowCache) ClearSoldOut(ctx context.Context, eventID string) error {
	s.cleared = append(s.cleared, eventID)
	return nil
}

func TestProcessorWithCache(t *testing.T) {
	cache := &stubWorkflowCache{}
	processor := workflow.NewProcessor(nil, "approve").WithCache(cache)
	if processor == nil {
		t.Fatal("expected non-nil processor")
	}
}
