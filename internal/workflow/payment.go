package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/theRiddler9/OrderFlow/internal/config"
	"github.com/theRiddler9/OrderFlow/internal/events"
	"github.com/theRiddler9/OrderFlow/internal/tracing"
)

const (
	paymentRequestedEvent = "payment.requested.v1"
	paymentSucceededEvent = "payment.succeeded.v1"
	paymentDeclinedEvent  = "payment.declined.v1"

	scenarioApprove = "approve"
	scenarioDecline = "decline"

	// Reasons recorded on a declined attempt.
	reasonCardDeclined       = "card_declined"
	reasonHoldExpired        = "hold_expired"
	reasonReservationNotHeld = "reservation_not_held"
)

// PaymentRequested is the payload emitted when a customer starts a payment attempt.
type PaymentRequested struct {
	PaymentID     string `json:"payment_id"`
	ReservationID string `json:"reservation_id"`
	EventID       string `json:"event_id"`
	CustomerID    string `json:"customer_id"`
	Quantity      int    `json:"quantity"`
	Scenario      string `json:"scenario"`
}

// ParsePaymentRequested validates a payment.requested.v1 event payload. Failures wrap
// ErrPoisonMessage: a malformed payload will not become valid by retrying.
func ParsePaymentRequested(event events.Envelope) (PaymentRequested, error) {
	if event.Type != paymentRequestedEvent {
		return PaymentRequested{}, fmt.Errorf("%w: event type %q is not %s", ErrPoisonMessage, event.Type, paymentRequestedEvent)
	}

	var payload PaymentRequested
	if err := json.Unmarshal(event.Payload, &payload); err != nil {
		return PaymentRequested{}, fmt.Errorf("decode payment requested payload: %w", err)
	}
	if _, err := uuid.Parse(payload.PaymentID); err != nil {
		return PaymentRequested{}, fmt.Errorf("%w: payment_id must be a UUID", ErrPoisonMessage)
	}
	if _, err := uuid.Parse(payload.ReservationID); err != nil {
		return PaymentRequested{}, fmt.Errorf("%w: reservation_id must be a UUID", ErrPoisonMessage)
	}
	if payload.Scenario != scenarioApprove && payload.Scenario != scenarioDecline {
		return PaymentRequested{}, fmt.Errorf("%w: scenario must be approve or decline", ErrPoisonMessage)
	}
	return payload, nil
}

func isPaymentOutcomeEvent(eventType string) bool {
	return eventType == paymentSucceededEvent || eventType == paymentDeclinedEvent
}

// handlePaymentRequested plays the simulated payment gateway for one attempt.
//
// It is safe under at-least-once delivery: the processed_events insert deduplicates
// redelivery, and the attempt is only settled while it is still "processing", so an
// attempt already settled by the expiry sweeper is acknowledged without effect.
func (processor *Processor) handlePaymentRequested(ctx context.Context, event events.Envelope) error {
	if processor.paymentMode != config.PaymentModeCheckout {
		// Nothing creates these in auto mode. Acknowledge rather than retry forever.
		if processor.metrics != nil {
			processor.metrics.WorkerEventsProcessedTotal.WithLabelValues(event.Type, "ignored").Inc()
		}
		return nil
	}
	if processor.database == nil {
		return fmt.Errorf("workflow database is required")
	}

	payload, err := ParsePaymentRequested(event)
	if err != nil {
		return err
	}
	span := trace.SpanFromContext(ctx)
	span.SetAttributes(
		attribute.String("payment.id", payload.PaymentID),
		attribute.String("payment.scenario", payload.Scenario),
	)

	// The gateway "takes a moment". Wait before opening a transaction so no row locks
	// are held while sleeping, and so a cancelled worker stops promptly.
	if processor.paymentDelay > 0 {
		timer := time.NewTimer(processor.paymentDelay)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
		}
	}

	tx, err := processor.database.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin payment transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	var processedID string
	err = tx.QueryRow(ctx, `
		INSERT INTO processed_events (event_id)
		VALUES ($1)
		ON CONFLICT DO NOTHING
		RETURNING event_id`, event.ID).Scan(&processedID)
	if errors.Is(err, pgx.ErrNoRows) {
		return tx.Commit(ctx)
	}
	if err != nil {
		return fmt.Errorf("record processed event: %w", err)
	}

	// Lock order is reservation then payment, matching the API and the expiry sweeper.
	// Inventory figures come from the reservation row, not from the event payload.
	held := HeldReservation{ReservationID: payload.ReservationID}
	var status string
	var expiresAt *time.Time
	err = tx.QueryRow(ctx, `
		SELECT event_id, customer_id, quantity, status, expires_at, created_at
		FROM reservations
		WHERE id = $1
		FOR UPDATE`, payload.ReservationID).Scan(&held.EventID, &held.CustomerID, &held.Quantity, &status, &expiresAt, &held.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return tx.Commit(ctx)
	}
	if err != nil {
		return fmt.Errorf("lock reservation: %w", err)
	}

	var paymentStatus string
	err = tx.QueryRow(ctx, `
		SELECT status
		FROM payments
		WHERE id = $1 AND reservation_id = $2
		FOR UPDATE`, payload.PaymentID, payload.ReservationID).Scan(&paymentStatus)
	if errors.Is(err, pgx.ErrNoRows) {
		return tx.Commit(ctx)
	}
	if err != nil {
		return fmt.Errorf("lock payment: %w", err)
	}
	if paymentStatus != "processing" {
		// Already settled, for example declined by the expiry sweeper.
		if processor.metrics != nil {
			processor.metrics.WorkerEventsProcessedTotal.WithLabelValues(event.Type, "ignored").Inc()
		}
		return tx.Commit(ctx)
	}

	// outcome is the metric label; reason is set only when the attempt is declined.
	var outcome, reason string
	switch {
	case status != "held":
		outcome, reason = "declined", reasonReservationNotHeld
	case expiresAt == nil || !expiresAt.After(time.Now().UTC()):
		// The hold ran out before the gateway answered. Release the tickets.
		if err := transitionReservation(ctx, tx, held, "expired", reasonHoldExpired); err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			return err
		}
		outcome, reason = "expired", reasonHoldExpired
	case payload.Scenario == scenarioDecline:
		// A decline leaves the hold open so the customer can try again before it expires.
		outcome, reason = "declined", reasonCardDeclined
	default:
		if err := transitionReservation(ctx, tx, held, "confirmed", ""); err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			return err
		}
		outcome = "confirmed"
	}

	if err := settlePayment(ctx, tx, payload, held, outcome, reason); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}

	span.SetAttributes(attribute.String("payment.outcome", outcome))
	span.SetStatus(codes.Ok, "payment settled")
	if processor.metrics != nil {
		processor.metrics.WorkerEventsProcessedTotal.WithLabelValues(event.Type, outcome).Inc()
		if outcome == "confirmed" || outcome == "expired" {
			processor.metrics.SagaLatencySeconds.WithLabelValues(outcome).Observe(time.Since(held.CreatedAt).Seconds())
		}
	}
	if outcome == "expired" && processor.cache != nil {
		_ = processor.cache.ClearSoldOut(ctx, held.EventID)
	}
	return nil
}

// settlePayment records the attempt's final state and emits its outcome event in the
// same transaction. outcome "confirmed" maps to a succeeded attempt; everything else
// (declined, or expired while paying) is a declined attempt with a reason.
func settlePayment(ctx context.Context, tx pgx.Tx, payload PaymentRequested, held HeldReservation, outcome, reason string) error {
	eventType := paymentDeclinedEvent
	status := "declined"
	if outcome == "confirmed" {
		eventType, status = paymentSucceededEvent, "succeeded"
		if _, err := tx.Exec(ctx, `
			UPDATE payments SET status = 'succeeded', completed_at = NOW()
			WHERE id = $1`, payload.PaymentID); err != nil {
			return fmt.Errorf("mark payment succeeded: %w", err)
		}
	} else if _, err := tx.Exec(ctx, `
		UPDATE payments SET status = 'declined', failure_reason = $2, completed_at = NOW()
		WHERE id = $1`, payload.PaymentID, reason); err != nil {
		return fmt.Errorf("mark payment declined: %w", err)
	}

	outcomePayload := map[string]any{
		"payment_id":     payload.PaymentID,
		"reservation_id": held.ReservationID,
		"event_id":       held.EventID,
		"customer_id":    held.CustomerID,
		"quantity":       held.Quantity,
		"status":         status,
		"reason":         nullableReason(reason),
	}
	if tp, ts := tracing.InjectTraceParent(ctx); tp != "" {
		outcomePayload["traceparent"] = tp
		if ts != "" {
			outcomePayload["tracestate"] = ts
		}
	}
	encoded, err := json.Marshal(outcomePayload)
	if err != nil {
		return fmt.Errorf("encode payment outcome event: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO outbox_events (id, aggregate_id, event_type, payload)
		VALUES ($1, $2, $3, $4)`, uuid.New(), held.ReservationID, eventType, encoded); err != nil {
		return fmt.Errorf("write payment outcome outbox event: %w", err)
	}
	return nil
}
