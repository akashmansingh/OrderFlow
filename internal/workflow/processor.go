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
	"github.com/theRiddler9/OrderFlow/internal/events"
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
	cache         inventoryCache
}

func NewProcessor(database *pgxpool.Pool, paymentResult string) *Processor {
	return &Processor{database: database, paymentResult: paymentResult}
}

func (processor *Processor) WithCache(cache inventoryCache) *Processor {
	processor.cache = cache
	return processor
}

// Handle processes a single event with at-least-once delivery safety.
func (processor *Processor) Handle(ctx context.Context, event events.Envelope) error {
	if isTerminalEvent(event.Type) {
		return nil
	}
	if event.Type != "reservation.held.v1" {
		return fmt.Errorf("unsupported event type %q", event.Type)
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
	err = tx.QueryRow(ctx, `
		SELECT status, expires_at
		FROM reservations
		WHERE id = $1
		FOR UPDATE`, payload.ReservationID).Scan(&status, &expiresAt)
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
	finalStatus, reason := paymentApprove, ""
	if expiresAt == nil || !expiresAt.After(now) {
		finalStatus, reason = "expired", "hold_expired"
	} else if processor.paymentResult == paymentReject {
		finalStatus, reason = "cancelled", "payment_rejected"
	}

	if err := transitionReservation(ctx, tx, payload, finalStatus, reason); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	if (finalStatus == "cancelled" || finalStatus == "expired") && processor.cache != nil {
		_ = processor.cache.ClearSoldOut(ctx, payload.EventID)
	}
	return nil
}

func transitionReservation(ctx context.Context, tx pgx.Tx, payload HeldReservation, status, reason string) error {
	if status == paymentApprove {
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
	terminalPayload, err := json.Marshal(map[string]any{
		"reservation_id": payload.ReservationID,
		"event_id":       payload.EventID,
		"customer_id":    payload.CustomerID,
		"quantity":       payload.Quantity,
		"status":         status,
		"reason":         nullableReason(reason),
	})
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
