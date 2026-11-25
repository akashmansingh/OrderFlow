package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/theRiddler9/OrderFlow/internal/config"
	"github.com/theRiddler9/OrderFlow/internal/events"
	"github.com/theRiddler9/OrderFlow/internal/metrics"
	"github.com/theRiddler9/OrderFlow/internal/tracing"
)

const (
	paymentApprove = "approve"
	paymentReject  = "reject"
)

type inventoryCache interface {
	ClearSoldOut(ctx context.Context, eventID string) error
}

// Processor applies held reservation events to authoritative PostgreSQL state.
type Processor struct {
	database      *pgxpool.Pool
	paymentResult string
	paymentMode   string
	paymentDelay  time.Duration
	cache         inventoryCache
	metrics       *metrics.Metrics
}

func NewProcessor(database *pgxpool.Pool, paymentResult string) *Processor {
	return &Processor{database: database, paymentResult: paymentResult, paymentMode: config.PaymentModeAuto}
}

// WithPayments selects how holds are settled. In checkout mode a hold waits for the
// customer to pay and the simulated gateway takes delay to answer; in auto mode the
// worker settles every hold itself using the configured simulated result.
func (processor *Processor) WithPayments(mode string, delay time.Duration) *Processor {
	processor.paymentMode = mode
	processor.paymentDelay = delay
	return processor
}

func (processor *Processor) WithCache(cache inventoryCache) *Processor {
	processor.cache = cache
	return processor
}

// WithMetrics attaches a Prometheus metrics collector to the processor.
func (processor *Processor) WithMetrics(m *metrics.Metrics) *Processor {
	processor.metrics = m
	return processor
}

// Handle processes a single event with at-least-once delivery safety.
func (processor *Processor) Handle(ctx context.Context, event events.Envelope) error {
	tr := tracing.Tracer("orderflow.workflow")
	ctx, span := tr.Start(ctx, "workflow.ProcessSaga",
		trace.WithAttributes(
			attribute.String("reservation.id", event.AggregateID),
			attribute.String("event.type", event.Type),
			attribute.String("payment.simulation", processor.paymentResult),
		),
	)
	defer span.End()

	start := time.Now()
	defer func() {
		if processor.metrics != nil {
			processor.metrics.WorkerEventDurationSeconds.WithLabelValues(event.Type).Observe(time.Since(start).Seconds())
		}
	}()

	if isTerminalEvent(event.Type) || isPaymentOutcomeEvent(event.Type) {
		if processor.metrics != nil {
			processor.metrics.WorkerEventsProcessedTotal.WithLabelValues(event.Type, "ignored").Inc()
		}
		return nil
	}
	if event.Type == paymentRequestedEvent {
		return processor.handlePaymentRequested(ctx, event)
	}
	if event.Type != "reservation.held.v1" {
		return fmt.Errorf("unsupported event type %q", event.Type)
	}
	if processor.paymentMode == config.PaymentModeCheckout {
		// The customer settles this hold by paying; expiry releases it if they do not.
		if processor.metrics != nil {
			processor.metrics.WorkerEventsProcessedTotal.WithLabelValues(event.Type, "awaiting_payment").Inc()
		}
		return nil
	}
	if processor.database == nil {
		return fmt.Errorf("workflow database is required")
	}

	payload, err := ParseHeldReservation(event)
	if err != nil {
		return err
	}

	tx, err := processor.database.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin workflow transaction: %w", err)
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

	var status string
	var expiresAt *time.Time
	var createdAt time.Time
	err = tx.QueryRow(ctx, `
		SELECT status, expires_at, created_at
		FROM reservations
		WHERE id = $1
		FOR UPDATE`, payload.ReservationID).Scan(&status, &expiresAt, &createdAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return tx.Commit(ctx)
	}
	if err != nil {
		return fmt.Errorf("lock reservation: %w", err)
	}
	if status != "held" {
		return tx.Commit(ctx)
	}

	now := time.Now().UTC()
	finalStatus, reason := "confirmed", ""
	if expiresAt == nil || !expiresAt.After(now) {
		finalStatus, reason = "expired", "hold_expired"
	} else if processor.paymentResult == paymentReject {
		finalStatus, reason = "cancelled", "payment_rejected"
	}

	if err := transitionReservation(ctx, tx, payload, finalStatus, reason); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}
	span.SetAttributes(attribute.String("reservation.final_status", finalStatus))
	span.SetStatus(codes.Ok, "saga transition committed")
	if processor.metrics != nil {
		processor.metrics.WorkerEventsProcessedTotal.WithLabelValues(event.Type, finalStatus).Inc()
		processor.metrics.SagaLatencySeconds.WithLabelValues(finalStatus).Observe(time.Since(createdAt).Seconds())
	}
	if (finalStatus == "cancelled" || finalStatus == "expired") && processor.cache != nil {
		_ = processor.cache.ClearSoldOut(ctx, payload.EventID)
	}
	return nil
}

func transitionReservation(ctx context.Context, tx pgx.Tx, payload HeldReservation, status, reason string) error {
	if status == "confirmed" {
		if _, err := tx.Exec(ctx, `
			UPDATE events
			SET held_quantity = held_quantity - $2,
				confirmed_quantity = confirmed_quantity + $2,
				updated_at = NOW()
			WHERE id = $1 AND held_quantity >= $2`, payload.EventID, payload.Quantity); err != nil {
			return fmt.Errorf("confirm inventory: %w", err)
		}
	} else if _, err := tx.Exec(ctx, `
		UPDATE events
		SET held_quantity = held_quantity - $2, updated_at = NOW()
		WHERE id = $1 AND held_quantity >= $2`, payload.EventID, payload.Quantity); err != nil {
		return fmt.Errorf("release inventory: %w", err)
	}

	if _, err := tx.Exec(ctx, `UPDATE reservations SET status = $2, expires_at = NULL, updated_at = NOW() WHERE id = $1`, payload.ReservationID, status); err != nil {
		return fmt.Errorf("update reservation status: %w", err)
	}

	eventType := "reservation." + status + ".v1"
	terminalPayloadMap := map[string]any{
		"reservation_id": payload.ReservationID,
		"event_id":       payload.EventID,
		"customer_id":    payload.CustomerID,
		"quantity":       payload.Quantity,
		"status":         status,
		"reason":         nullableReason(reason),
	}
	if tp, ts := tracing.InjectTraceParent(ctx); tp != "" {
		terminalPayloadMap["traceparent"] = tp
		if ts != "" {
			terminalPayloadMap["tracestate"] = ts
		}
	}
	terminalPayload, err := json.Marshal(terminalPayloadMap)
	if err != nil {
		return fmt.Errorf("encode terminal event: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO outbox_events (id, aggregate_id, event_type, payload)
		VALUES ($1, $2, $3, $4)`, uuid.New(), payload.ReservationID, eventType, terminalPayload); err != nil {
		return fmt.Errorf("write terminal outbox event: %w", err)
	}
	return nil
}

func isTerminalEvent(eventType string) bool {
	return eventType == "reservation.confirmed.v1" || eventType == "reservation.cancelled.v1" || eventType == "reservation.expired.v1"
}

func nullableReason(reason string) any {
	if reason == "" {
		return nil
	}
	return reason
}

// HeldReservation is the payload emitted when the Reservation API holds inventory.
type HeldReservation struct {
	ReservationID string    `json:"reservation_id"`
	EventID       string    `json:"event_id"`
	CustomerID    string    `json:"customer_id"`
	Quantity      int       `json:"quantity"`
	ExpiresAt     time.Time `json:"expires_at"`
	CreatedAt     time.Time `json:"created_at,omitempty"`
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
