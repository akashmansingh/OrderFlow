package events

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Envelope is the versioned message shared between the PostgreSQL outbox and Redpanda.
type Envelope struct {
	ID          string          `json:"id"`
	Type        string          `json:"type"`
	AggregateID string          `json:"aggregate_id"`
	OccurredAt  time.Time       `json:"occurred_at"`
	Payload     json.RawMessage `json:"payload"`
}

// ParseEnvelope decodes and validates one event message.
func ParseEnvelope(raw []byte) (Envelope, error) {
	var event Envelope
	if err := json.Unmarshal(raw, &event); err != nil {
		return Envelope{}, fmt.Errorf("decode event envelope: %w", err)
	}
	if _, err := uuid.Parse(event.ID); err != nil {
		return Envelope{}, fmt.Errorf("event id must be a UUID")
	}
	if strings.TrimSpace(event.Type) == "" {
		return Envelope{}, fmt.Errorf("event type is required")
	}
	if _, err := uuid.Parse(event.AggregateID); err != nil {
		return Envelope{}, fmt.Errorf("event aggregate_id must be a UUID")
	}
	if event.OccurredAt.IsZero() {
		return Envelope{}, fmt.Errorf("event occurred_at is required")
	}
	if len(event.Payload) == 0 || !json.Valid(event.Payload) {
		return Envelope{}, fmt.Errorf("event payload must be valid JSON")
	}

	return event, nil
}
