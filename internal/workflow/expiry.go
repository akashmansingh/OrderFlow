package workflow

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Expiry releases inventory held by reservations whose hold has elapsed.
type Expiry struct {
	database *pgxpool.Pool
	cache    inventoryCache
}

func NewExpiry(database *pgxpool.Pool) *Expiry {
	return &Expiry{database: database}
}

func (expiry *Expiry) WithCache(cache inventoryCache) *Expiry {
	expiry.cache = cache
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
		SELECT id, event_id, customer_id, quantity, expires_at
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
		if err := rows.Scan(&reservation.ReservationID, &reservation.EventID, &reservation.CustomerID, &reservation.Quantity, &reservation.ExpiresAt); err != nil {
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
	if expiry.cache != nil {
		for _, reservation := range expired {
			_ = expiry.cache.ClearSoldOut(ctx, reservation.EventID)
		}
	}
	return nil
}
