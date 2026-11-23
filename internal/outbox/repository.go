package outbox

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/theRiddler9/OrderFlow/internal/events"
)

// PostgresStore persists outbox state in PostgreSQL.
type PostgresStore struct {
	database *pgxpool.Pool
	lease    time.Duration
}

func NewPostgresStore(database *pgxpool.Pool, lease time.Duration) *PostgresStore {
	return &PostgresStore{database: database, lease: lease}
}

func (store *PostgresStore) ClaimNext(ctx context.Context) (events.Envelope, bool, error) {
	const query = `
		WITH candidate AS (
			SELECT id
			FROM outbox_events
			WHERE published_at IS NULL
				AND (locked_at IS NULL OR locked_at < NOW() - ($1 * INTERVAL '1 microsecond'))
			ORDER BY occurred_at
			LIMIT 1
			FOR UPDATE SKIP LOCKED
		)
		UPDATE outbox_events AS event
		SET locked_at = NOW()
		FROM candidate
		WHERE event.id = candidate.id
		RETURNING event.id, event.event_type, event.aggregate_id, event.occurred_at, event.payload`

	var event events.Envelope
	err := store.database.QueryRow(ctx, query, store.lease.Microseconds()).Scan(
		&event.ID,
		&event.Type,
		&event.AggregateID,
		&event.OccurredAt,
		&event.Payload,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return events.Envelope{}, false, nil
	}
	if err != nil {
		return events.Envelope{}, false, fmt.Errorf("claim outbox event: %w", err)
	}
	return event, true, nil
}

func (store *PostgresStore) MarkPublished(ctx context.Context, eventID string) error {
	command, err := store.database.Exec(ctx,
		`UPDATE outbox_events SET published_at = NOW(), locked_at = NULL WHERE id = $1 AND published_at IS NULL`,
		eventID,
	)
	if err != nil {
		return fmt.Errorf("mark outbox event published: %w", err)
	}
	if command.RowsAffected() != 1 {
		return fmt.Errorf("outbox event %s was not available to mark published", eventID)
	}
	return nil
}

func (store *PostgresStore) RecordPublishFailure(ctx context.Context, eventID string) error {
	command, err := store.database.Exec(ctx,
		`UPDATE outbox_events SET publish_attempts = publish_attempts + 1, locked_at = NULL WHERE id = $1 AND published_at IS NULL`,
		eventID,
	)
	if err != nil {
		return fmt.Errorf("record outbox publish failure: %w", err)
	}
	if command.RowsAffected() != 1 {
		return fmt.Errorf("outbox event %s was not available to record failure", eventID)
	}
	return nil
}

// CountPending returns the number of outbox events waiting to be published.
func (store *PostgresStore) CountPending(ctx context.Context) (int64, error) {
	var count int64
	err := store.database.QueryRow(ctx, `SELECT COUNT(*) FROM outbox_events WHERE published_at IS NULL`).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count pending outbox events: %w", err)
	}
	return count, nil
}

// ReportBacklog returns both the pending count and the age in seconds of the oldest unpublished record.
func (store *PostgresStore) ReportBacklog(ctx context.Context) (int64, float64, error) {
	var count int64
	var oldestAge float64
	const query = `
		SELECT COUNT(*), COALESCE(EXTRACT(EPOCH FROM (NOW() - MIN(occurred_at))), 0)
		FROM outbox_events
		WHERE published_at IS NULL`
	err := store.database.QueryRow(ctx, query).Scan(&count, &oldestAge)
	if err != nil {
		return 0, 0, fmt.Errorf("report outbox backlog: %w", err)
	}
	return count, oldestAge, nil
}
