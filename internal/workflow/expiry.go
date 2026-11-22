package workflow

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/theRiddler9/OrderFlow/internal/metrics"
)

// Expiry releases inventory held by reservations whose hold has elapsed.
type Expiry struct {
	database *pgxpool.Pool
	cache    inventoryCache
	metrics  *metrics.Metrics
}

func NewExpiry(database *pgxpool.Pool) *Expiry {
	return &Expiry{database: database}
}

func (expiry *Expiry) WithCache(cache inventoryCache) *Expiry {
	expiry.cache = cache
	return expiry
}

// WithMetrics attaches a Prometheus metrics collector to the expiry sweeper.
func (expiry *Expiry) WithMetrics(m *metrics.Metrics) *Expiry {
	expiry.metrics = m
	return expiry
}

// RunOnce expires one locked batch of held reservations.
func (expiry *Expiry) RunOnce(ctx context.Context) error {
	if expiry.database == nil {
		return fmt.Errorf("workflow database is required")
	}

	tx, err := expiry.database.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin expiry transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	rows, err := tx.Query(ctx, `
		SELECT id, event_id, customer_id, quantity, expires_at, created_at
		FROM reservations
		WHERE status = 'held' AND expires_at <= NOW()
		ORDER BY expires_at
		LIMIT 100
		FOR UPDATE SKIP LOCKED`)
	if err != nil {
		return fmt.Errorf("find expired reservations: %w", err)
	}
	defer rows.Close()

	var expired []HeldReservation
	for rows.Next() {
		var reservation HeldReservation
		if err := rows.Scan(&reservation.ReservationID, &reservation.EventID, &reservation.CustomerID, &reservation.Quantity, &reservation.ExpiresAt, &reservation.CreatedAt); err != nil {
			return fmt.Errorf("scan expired reservation: %w", err)
		}
		expired = append(expired, reservation)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate expired reservations: %w", err)
	}
	rows.Close()

	for _, reservation := range expired {
		if err := transitionReservation(ctx, tx, reservation, "expired", "hold_expired"); err != nil {
			return err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit expiry transaction: %w", err)
	}
	if expiry.metrics != nil && len(expired) > 0 {
		expiry.metrics.WorkerEventsProcessedTotal.WithLabelValues("reservation.held.v1", "expired").Add(float64(len(expired)))
		for _, reservation := range expired {
			expiry.metrics.SagaLatencySeconds.WithLabelValues("expired").Observe(time.Since(reservation.CreatedAt).Seconds())
		}
	}
	if expiry.cache != nil {
		cleared := make(map[string]struct{}, len(expired))
		for _, reservation := range expired {
			if _, exists := cleared[reservation.EventID]; !exists {
				cleared[reservation.EventID] = struct{}{}
				_ = expiry.cache.ClearSoldOut(ctx, reservation.EventID)
			}
		}
	}
	return nil
}
