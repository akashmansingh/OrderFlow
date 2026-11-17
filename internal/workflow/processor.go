package workflow

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/theRiddler9/OrderFlow/internal/events"
)

// HeldReservation is the payload emitted when the Reservation API holds inventory.
type HeldReservation struct {
	ReservationID string    `json:"reservation_id"`
	EventID       string    `json:"event_id"`
	CustomerID    string    `json:"customer_id"`
	Quantity      int       `json:"quantity"`
	ExpiresAt     time.Time `json:"expires_at"`
}

// ParseHeldReservation validates a reservation.held.v1 event payload.
func ParseHeldReservation(event events.Envelope) (HeldReservation, error) {
	if event.Type != "reservation.held.v1" {
		return HeldReservation{}, fmt.Errorf("event type %q is not reservation.held.v1", event.Type)
	}

	var payload HeldReservation
	if err := json.Unmarshal(event.Payload, &payload); err != nil {
		return HeldReservation{}, fmt.Errorf("decode held reservation payload: %w", err)
	}
	if _, err := uuid.Parse(payload.ReservationID); err != nil {
		return HeldReservation{}, fmt.Errorf("reservation_id must be a UUID")
	}
	if _, err := uuid.Parse(payload.EventID); err != nil {
		return HeldReservation{}, fmt.Errorf("event_id must be a UUID")
	}
	if _, err := uuid.Parse(payload.CustomerID); err != nil {
		return HeldReservation{}, fmt.Errorf("customer_id must be a UUID")
	}
	if payload.Quantity <= 0 {
		return HeldReservation{}, fmt.Errorf("quantity must be positive")
	}
	if payload.ExpiresAt.IsZero() {
		return HeldReservation{}, fmt.Errorf("expires_at is required")
	}
	return payload, nil
}
