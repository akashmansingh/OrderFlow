package reservation

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
	"github.com/theRiddler9/OrderFlow/internal/tracing"
)

// Simulated gateway outcomes a client may request. No card data is accepted or stored.
const (
	PaymentScenarioApprove = "approve"
	PaymentScenarioDecline = "decline"
)

// Payment states reported on a reservation while PAYMENT_MODE=checkout.
const (
	PaymentAwaiting   = "awaiting"
	PaymentProcessing = "processing"
	PaymentDeclined   = "declined"
	PaymentSucceeded  = "succeeded"
)

var (
	ErrInvalidPaymentScenario = errors.New("scenario must be approve or decline")
	ErrReservationNotPayable  = errors.New("reservation is not awaiting payment")
	ErrHoldExpired            = errors.New("reservation hold has expired")
	ErrPaymentInProgress      = errors.New("a payment is already being processed for this reservation")
	ErrPaymentsAutomatic      = errors.New("payments are settled automatically in this environment")
)

// Payment summarises the payment state of a reservation. It reflects the latest attempt.
type Payment struct {
	Status        string `json:"status"`
	Attempts      int    `json:"attempts"`
	PaymentID     string `json:"payment_id,omitempty"`
	FailureReason string `json:"failure_reason,omitempty"`
}

// PayReservationInput starts one payment attempt for a held reservation.
type PayReservationInput struct {
	ReservationID  string
	CustomerID     string
	Scenario       string
	IdempotencyKey string
}

// WithPaymentMode selects whether holds are settled automatically or by the customer paying.
func (service *Service) WithPaymentMode(mode string) *Service {
	service.paymentMode = mode
	return service
}

func (service *Service) checkoutEnabled() bool {
	return service.paymentMode == config.PaymentModeCheckout
}

// PayReservation records a payment attempt and its outbox event in one transaction.
// The worker settles the attempt asynchronously, so a successful call means "accepted",
// not "paid". Retrying with the same Idempotency-Key returns the current state without
// creating a second attempt.
func (service *Service) PayReservation(ctx context.Context, input PayReservationInput) (Reservation, error) {
	tr := tracing.Tracer("reservation.service")
	ctx, span := tr.Start(ctx, "reservation.PayReservation",
		trace.WithAttributes(
			attribute.String("reservation.id", input.ReservationID),
			attribute.String("payment.scenario", input.Scenario),
		),
	)
	defer span.End()

	fail := func(err error) (Reservation, error) {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return Reservation{}, err
	}

	if input.Scenario == "" {
		input.Scenario = PaymentScenarioApprove
	}
	if input.Scenario != PaymentScenarioApprove && input.Scenario != PaymentScenarioDecline {
		return fail(ErrInvalidPaymentScenario)
	}
	if _, err := uuid.Parse(input.ReservationID); err != nil {
		return fail(ErrReservationNotFound)
	}
	if _, err := uuid.Parse(input.CustomerID); err != nil {
		return fail(ErrReservationNotFound)
	}
	if input.IdempotencyKey == "" {
		return fail(fmt.Errorf("idempotency key is required"))
	}

	tx, err := service.database.Begin(ctx)
	if err != nil {
		return fail(fmt.Errorf("begin payment transaction: %w", err))
	}
	defer tx.Rollback(ctx)

	// The worker takes the same row lock, so a pay request and the settlement of an
	// earlier attempt for this reservation cannot interleave.
	reservation, err := scanReservation(tx.QueryRow(ctx, `
		SELECT id, event_id, customer_id, quantity, status, expires_at, created_at
		FROM reservations
		WHERE id = $1
		FOR UPDATE`, input.ReservationID))
	if errors.Is(err, pgx.ErrNoRows) {
		return fail(ErrReservationNotFound)
	}
	if err != nil {
		return fail(fmt.Errorf("lock reservation: %w", err))
	}
	// A reservation owned by someone else is reported as not found so ownership cannot be probed.
	if reservation.CustomerID != input.CustomerID {
		return fail(ErrReservationNotFound)
	}

	// Replays are answered before any state check, so a client retrying after a lost
	// response still gets an answer once the reservation has moved on.
	var existingScenario string
	err = tx.QueryRow(ctx,
		`SELECT scenario FROM payments WHERE reservation_id = $1 AND idempotency_key = $2`,
		input.ReservationID, input.IdempotencyKey,
	).Scan(&existingScenario)
	if err == nil {
		if existingScenario != input.Scenario {
			return fail(ErrIdempotencyConflict)
		}
		replayed, err := service.withPayment(ctx, tx, reservation)
		if err != nil {
			return fail(err)
		}
		return replayed, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return fail(fmt.Errorf("read payment idempotency key: %w", err))
	}

	if !service.checkoutEnabled() {
		return fail(ErrPaymentsAutomatic)
	}
	if reservation.Status != "held" {
		return fail(ErrReservationNotPayable)
	}
	if reservation.ExpiresAt == nil || !reservation.ExpiresAt.After(time.Now().UTC()) {
		return fail(ErrHoldExpired)
	}

	var inFlight bool
	if err := tx.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM payments WHERE reservation_id = $1 AND status = 'processing')`,
		input.ReservationID,
	).Scan(&inFlight); err != nil {
		return fail(fmt.Errorf("check in-flight payment: %w", err))
	}
	if inFlight {
		return fail(ErrPaymentInProgress)
	}

	paymentID := uuid.New()
	if _, err := tx.Exec(ctx, `
		INSERT INTO payments (id, reservation_id, customer_id, scenario, status, idempotency_key)
		VALUES ($1, $2, $3, $4, 'processing', $5)`,
		paymentID, input.ReservationID, input.CustomerID, input.Scenario, input.IdempotencyKey,
	); err != nil {
		return fail(fmt.Errorf("create payment attempt: %w", err))
	}

	payloadMap := map[string]any{
		"payment_id":     paymentID,
		"reservation_id": reservation.ID,
		"event_id":       reservation.EventID,
		"customer_id":    reservation.CustomerID,
		"quantity":       reservation.Quantity,
		"scenario":       input.Scenario,
	}
	if tp, ts := tracing.InjectTraceParent(ctx); tp != "" {
		payloadMap["traceparent"] = tp
		if ts != "" {
			payloadMap["tracestate"] = ts
		}
	}
	payload, err := json.Marshal(payloadMap)
	if err != nil {
		return fail(fmt.Errorf("encode payment event: %w", err))
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO outbox_events (id, aggregate_id, event_type, payload) VALUES ($1, $2, $3, $4)`,
		uuid.New(), reservation.ID, "payment.requested.v1", payload,
	); err != nil {
		return fail(fmt.Errorf("write payment outbox event: %w", err))
	}

	accepted, err := service.withPayment(ctx, tx, reservation)
	if err != nil {
		return fail(err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fail(fmt.Errorf("commit payment transaction: %w", err))
	}

	span.SetAttributes(attribute.String("payment.id", paymentID.String()))
	span.SetStatus(codes.Ok, "payment accepted")
	return accepted, nil
}

// withPayment attaches the latest payment attempt to a reservation. It is a no-op
// unless the service runs in checkout mode.
func (service *Service) withPayment(ctx context.Context, querier rowQuerier, reservation Reservation) (Reservation, error) {
	if !service.checkoutEnabled() {
		return reservation, nil
	}

	var (
		paymentID string
		status    string
		failure   *string
		attempts  int
	)
	err := querier.QueryRow(ctx, `
		SELECT id, status, failure_reason,
		       (SELECT COUNT(*) FROM payments WHERE reservation_id = $1)
		FROM payments
		WHERE reservation_id = $1
		ORDER BY created_at DESC, id DESC
		LIMIT 1`, reservation.ID).Scan(&paymentID, &status, &failure, &attempts)
	if errors.Is(err, pgx.ErrNoRows) {
		// Nothing attempted yet. Only a live hold is waiting for payment.
		if reservation.Status == "held" {
			reservation.Payment = &Payment{Status: PaymentAwaiting}
		}
		return reservation, nil
	}
	if err != nil {
		return Reservation{}, fmt.Errorf("read payment state: %w", err)
	}

	payment := &Payment{Status: status, Attempts: attempts, PaymentID: paymentID}
	if failure != nil {
		payment.FailureReason = *failure
	}
	reservation.Payment = payment
	return reservation, nil
}
