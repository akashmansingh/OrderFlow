package events_test

import (
	"strings"
	"testing"

	"github.com/theRiddler9/OrderFlow/internal/events"
)

func TestParseEnvelopeAcceptsCompleteEvent(t *testing.T) {
	raw := []byte(`{
		"id":"d2719c30-1edf-4c5f-9273-cb1f55a3797e",
		"type":"reservation.held.v1",
		"aggregate_id":"96e74b3e-01aa-469e-802d-f04e1dcfbdff",
		"occurred_at":"2026-11-17T12:00:00Z",
		"payload":{"reservation_id":"61c26b50-c02a-4a86-a46d-1f2ea1078b9f","quantity":2}
	}`)

	event, err := events.ParseEnvelope(raw)
	if err != nil {
		t.Fatalf("ParseEnvelope() error = %v", err)
	}
	if event.ID != "d2719c30-1edf-4c5f-9273-cb1f55a3797e" {
		t.Fatalf("ID = %q", event.ID)
	}
	if event.Type != "reservation.held.v1" {
		t.Fatalf("Type = %q", event.Type)
	}
	if !strings.Contains(string(event.Payload), `"quantity":2`) {
		t.Fatalf("Payload = %s", event.Payload)
	}
}

func TestParseEnvelopeRejectsMissingIdentity(t *testing.T) {
	_, err := events.ParseEnvelope([]byte(`{
		"type":"reservation.held.v1",
		"aggregate_id":"96e74b3e-01aa-469e-802d-f04e1dcfbdff",
		"occurred_at":"2026-11-17T12:00:00Z",
		"payload":{}
	}`))
	if err == nil || !strings.Contains(err.Error(), "id") {
		t.Fatalf("ParseEnvelope() error = %v, want missing id error", err)
	}

	_, err = events.ParseEnvelope([]byte(`{
		"id":"d2719c30-1edf-4c5f-9273-cb1f55a3797e",
		"aggregate_id":"96e74b3e-01aa-469e-802d-f04e1dcfbdff",
		"occurred_at":"2026-11-17T12:00:00Z",
		"payload":{}
	}`))
	if err == nil || !strings.Contains(err.Error(), "type") {
		t.Fatalf("ParseEnvelope() error = %v, want missing type error", err)
	}
}
