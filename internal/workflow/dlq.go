package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// ErrPoisonMessage represents an unrecoverable poison message.
var ErrPoisonMessage = errors.New("poison message")

// DLQEnvelope wraps a poisoned or exhausted event with diagnostic context.
type DLQEnvelope struct {
	ID              string    `json:"id"`
	OriginalPayload []byte    `json:"original_payload"`
	ErrorReason     string    `json:"error_reason"`
	RetryCount      int       `json:"retry_count"`
	FailedAt        time.Time `json:"failed_at"`
}

// DLQPublisher delivers a poisoned or failing event to the dead-letter topic.
type DLQPublisher interface {
	PublishDLQ(ctx context.Context, originalRecord []byte, reason string, retryCount int) error
}

// IsPoisonError returns true if an error is non-retryable (poison).
func IsPoisonError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, ErrPoisonMessage) {
		return true
	}
	var syntaxErr *json.SyntaxError
	var unmarshalTypeErr *json.UnmarshalTypeError
	return errors.As(err, &syntaxErr) || errors.As(err, &unmarshalTypeErr)
}

// FormatDLQEnvelope serializes an original record payload with diagnostic failure details.
func FormatDLQEnvelope(original []byte, reason string, retryCount int) ([]byte, error) {
	envelope := DLQEnvelope{
		ID:              uuid.New().String(),
		OriginalPayload: original,
		ErrorReason:     reason,
		RetryCount:      retryCount,
		FailedAt:        time.Now().UTC(),
	}
	encoded, err := json.Marshal(envelope)
	if err != nil {
		return nil, fmt.Errorf("marshal DLQ envelope: %w", err)
	}
	return encoded, nil
}
