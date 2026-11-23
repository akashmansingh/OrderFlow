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
	TraceParent string          `json:"traceparent,omitempty"`
	TraceState  string          `json:"tracestate,omitempty"`
}

// GetTraceParent returns the W3C traceparent string, checking the field directly
// or extracting from the payload JSON if present.
func (event Envelope) GetTraceParent() string {
	if event.TraceParent != "" {
		return event.TraceParent
	}
	if len(event.Payload) > 0 {
		var p struct {
			TraceParent string `json:"traceparent"`
		}
		_ = json.Unmarshal(event.Payload, &p)
		return p.TraceParent
	}
	return ""
}

// GetTraceState returns the W3C tracestate string if present.
func (event Envelope) GetTraceState() string {
	if event.TraceState != "" {
		return event.TraceState
	}
	if len(event.Payload) > 0 {
		var p struct {
			TraceState string `json:"tracestate"`
		}
		_ = json.Unmarshal(event.Payload, &p)
		return p.TraceState
	}
	return ""
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
