package workflow_test

import (
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

func TestParseHeldReservationRejectsMissingQuantity(t *testing.T) {
	_, err := workflow.ParseHeldReservation(events.Envelope{
		Type:    "reservation.held.v1",
		Payload: []byte(`{"reservation_id":"61c26b50-c02a-4a86-a46d-1f2ea1078b9f"}`),
	})
	if err == nil {
		t.Fatal("ParseHeldReservation() error = nil, want validation error")
	}
}
