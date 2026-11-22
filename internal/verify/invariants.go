package verify

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

// CheckResult represents the pass/fail outcome and diagnostic details of an invariant check.
type CheckResult struct {
	Name    string
	Passed  bool
	Message string
	Details string
}

// Verifier executes authoritative SQL invariant checks against PostgreSQL state.
type Verifier struct {
	pool *pgxpool.Pool
}

// NewVerifier creates a new database invariant verifier.
func NewVerifier(pool *pgxpool.Pool) *Verifier {
	return &Verifier{pool: pool}
}

// VerifyAll executes all 5 core invariants:
// 1. available >= 0
// 2. available + reserved + confirmed == initial stock per SKU
// 3. exactly one terminal state per reservation
// 4. outbox fully drained
// 5. inbox has no duplicate event IDs processed twice
func (v *Verifier) VerifyAll(ctx context.Context) ([]CheckResult, bool) {
	results := []CheckResult{
		v.CheckAvailableNonNegative(ctx),
		v.CheckInventoryConservation(ctx),
		v.CheckSingleTerminalState(ctx),
		v.CheckOutboxDrained(ctx),
		v.CheckNoDuplicateProcessedEvents(ctx),
	}

	allPassed := true
	for _, r := range results {
		if !r.Passed {
			allPassed = false
		}
	}

	return results, allPassed
}

// CheckAvailableNonNegative asserts that available stock is never negative: capacity - held_quantity - confirmed_quantity >= 0.
func (v *Verifier) CheckAvailableNonNegative(ctx context.Context) CheckResult {
	const query = `
		SELECT id, name, capacity, held_quantity, confirmed_quantity,
		       (capacity - held_quantity - confirmed_quantity) AS available
		FROM events
		WHERE (capacity - held_quantity - confirmed_quantity) < 0
		   OR held_quantity < 0
		   OR confirmed_quantity < 0`

	rows, err := v.pool.Query(ctx, query)
	if err != nil {
		return CheckResult{
			Name:    "available >= 0 (no oversell)",
			Passed:  false,
			Message: fmt.Sprintf("database query failed: %v", err),
		}
	}
	defer rows.Close()

	var violations []string
	for rows.Next() {
		var id, name string
		var cap, held, conf, avail int
		if err := rows.Scan(&id, &name, &cap, &held, &conf, &avail); err != nil {
			return CheckResult{
				Name:    "available >= 0 (no oversell)",
				Passed:  false,
				Message: fmt.Sprintf("scan failed: %v", err),
			}
		}
		violations = append(violations, fmt.Sprintf("Event %s (%s): capacity=%d, held=%d, confirmed=%d, available=%d", id, name, cap, held, conf, avail))
	}

	if len(violations) > 0 {
		return CheckResult{
			Name:    "available >= 0 (no oversell)",
			Passed:  false,
			Message: fmt.Sprintf("%d event(s) have negative available stock or invalid quantities", len(violations)),
			Details: strings.Join(violations, "\n"),
		}
	}

	return CheckResult{
		Name:    "available >= 0 (no oversell)",
		Passed:  true,
		Message: "all events have non-negative available stock",
	}
}

// CheckInventoryConservation asserts that:
// (capacity - held - confirmed) + held + confirmed == capacity
// and that events counters match the sum of reservations exactly.
func (v *Verifier) CheckInventoryConservation(ctx context.Context) CheckResult {
	const query = `
		SELECT e.id, e.name, e.capacity, e.held_quantity, e.confirmed_quantity,
		       COALESCE(r.sum_held, 0) AS sum_held,
		       COALESCE(r.sum_confirmed, 0) AS sum_confirmed
		FROM events e
		LEFT JOIN (
			SELECT event_id,
			       SUM(CASE WHEN status = 'held' THEN quantity ELSE 0 END) AS sum_held,
			       SUM(CASE WHEN status = 'confirmed' THEN quantity ELSE 0 END) AS sum_confirmed
			FROM reservations
			GROUP BY event_id
		) r ON e.id = r.event_id
		WHERE (e.capacity - e.held_quantity - e.confirmed_quantity) + e.held_quantity + e.confirmed_quantity != e.capacity
		   OR e.held_quantity != COALESCE(r.sum_held, 0)
		   OR e.confirmed_quantity != COALESCE(r.sum_confirmed, 0)`

	rows, err := v.pool.Query(ctx, query)
	if err != nil {
		return CheckResult{
			Name:    "available + reserved + confirmed == initial capacity",
			Passed:  false,
			Message: fmt.Sprintf("database query failed: %v", err),
		}
	}
	defer rows.Close()

	var discrepancies []string
	for rows.Next() {
		var id, name string
		var cap, held, conf, sumHeld, sumConf int
		if err := rows.Scan(&id, &name, &cap, &held, &conf, &sumHeld, &sumConf); err != nil {
			return CheckResult{
				Name:    "available + reserved + confirmed == initial capacity",
				Passed:  false,
				Message: fmt.Sprintf("scan failed: %v", err),
			}
		}
		discrepancies = append(discrepancies, fmt.Sprintf("Event %s (%s): capacity=%d, held_counter=%d vs sum_held=%d, confirmed_counter=%d vs sum_conf=%d", id, name, cap, held, sumHeld, conf, sumConf))
	}

	if len(discrepancies) > 0 {
		return CheckResult{
			Name:    "available + reserved + confirmed == initial capacity",
			Passed:  false,
			Message: fmt.Sprintf("%d event(s) have inventory sum or reservation sum mismatches", len(discrepancies)),
			Details: strings.Join(discrepancies, "\n"),
		}
	}

	return CheckResult{
		Name:    "available + reserved + confirmed == initial capacity",
		Passed:  true,
		Message: "inventory is perfectly conserved across events and reservations",
	}
}

// CheckSingleTerminalState asserts that each reservation has at most one terminal state event in the outbox.
func (v *Verifier) CheckSingleTerminalState(ctx context.Context) CheckResult {
	const query = `
		SELECT aggregate_id, COUNT(*) AS terminal_count
		FROM outbox_events
		WHERE event_type IN ('reservation.confirmed.v1', 'reservation.cancelled.v1', 'reservation.expired.v1')
		GROUP BY aggregate_id
		HAVING COUNT(*) > 1`

	rows, err := v.pool.Query(ctx, query)
	if err != nil {
		return CheckResult{
			Name:    "exactly one terminal state per reservation",
			Passed:  false,
			Message: fmt.Sprintf("database query failed: %v", err),
		}
	}
	defer rows.Close()

	var duplicateTerminals []string
	for rows.Next() {
		var resID string
		var count int
		if err := rows.Scan(&resID, &count); err != nil {
			return CheckResult{
				Name:    "exactly one terminal state per reservation",
				Passed:  false,
				Message: fmt.Sprintf("scan failed: %v", err),
			}
		}
		duplicateTerminals = append(duplicateTerminals, fmt.Sprintf("Reservation %s: %d terminal events in outbox", resID, count))
	}

	if len(duplicateTerminals) > 0 {
		return CheckResult{
			Name:    "exactly one terminal state per reservation",
			Passed:  false,
			Message: fmt.Sprintf("%d reservation(s) transitioned to multiple terminal states", len(duplicateTerminals)),
			Details: strings.Join(duplicateTerminals, "\n"),
		}
	}

	return CheckResult{
		Name:    "exactly one terminal state per reservation",
		Passed:  true,
		Message: "every reservation has at most one terminal state transition",
	}
}

// CheckOutboxDrained asserts that all outbox events have been published (published_at IS NOT NULL).
func (v *Verifier) CheckOutboxDrained(ctx context.Context) CheckResult {
	const query = `SELECT COUNT(*) FROM outbox_events WHERE published_at IS NULL`

	var unpublishedCount int64
	err := v.pool.QueryRow(ctx, query).Scan(&unpublishedCount)
	if err != nil {
		return CheckResult{
			Name:    "outbox fully drained",
			Passed:  false,
			Message: fmt.Sprintf("database query failed: %v", err),
		}
	}

	if unpublishedCount > 0 {
		return CheckResult{
			Name:    "outbox fully drained",
			Passed:  false,
			Message: fmt.Sprintf("%d unpublished event(s) remain in outbox table", unpublishedCount),
		}
	}

	return CheckResult{
		Name:    "outbox fully drained",
		Passed:  true,
		Message: "outbox table is fully drained (0 pending events)",
	}
}

// CheckNoDuplicateProcessedEvents asserts that no event ID in processed_events appears more than once.
func (v *Verifier) CheckNoDuplicateProcessedEvents(ctx context.Context) CheckResult {
	const query = `
		SELECT event_id, COUNT(*)
		FROM processed_events
		GROUP BY event_id
		HAVING COUNT(*) > 1`

	rows, err := v.pool.Query(ctx, query)
	if err != nil {
		return CheckResult{
			Name:    "inbox has no duplicate processed event IDs",
			Passed:  false,
			Message: fmt.Sprintf("database query failed: %v", err),
		}
	}
	defer rows.Close()

	var duplicates []string
	for rows.Next() {
		var eventID string
		var count int
		if err := rows.Scan(&eventID, &count); err != nil {
			return CheckResult{
				Name:    "inbox has no duplicate processed event IDs",
				Passed:  false,
				Message: fmt.Sprintf("scan failed: %v", err),
			}
		}
		duplicates = append(duplicates, fmt.Sprintf("Event %s processed %d times", eventID, count))
	}

	if len(duplicates) > 0 {
		return CheckResult{
			Name:    "inbox has no duplicate processed event IDs",
			Passed:  false,
			Message: fmt.Sprintf("%d event ID(s) processed multiple times", len(duplicates)),
			Details: strings.Join(duplicates, "\n"),
		}
	}

	return CheckResult{
		Name:    "inbox has no duplicate processed event IDs",
		Passed:  true,
		Message: "processed_events has zero duplicates (idempotent inbox)",
	}
}
