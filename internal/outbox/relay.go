package outbox

import (
	"context"
	"fmt"
	"time"

	"github.com/theRiddler9/OrderFlow/internal/events"
	"github.com/theRiddler9/OrderFlow/internal/metrics"
)

// Publisher publishes an outbox event to the configured message broker.
type Publisher interface {
	Publish(context.Context, events.Envelope) error
}

// BacklogReporter reports the number of pending outbox records.
type BacklogReporter interface {
	CountPending(context.Context) (int64, error)
}

// Store provides durable outbox state transitions.
type Store interface {
	ClaimNext(context.Context) (events.Envelope, bool, error)
	MarkPublished(context.Context, string) error
	RecordPublishFailure(context.Context, string) error
}

// Relay moves committed database events to the message broker.
type Relay struct {
	store        Store
	publisher    Publisher
	pollInterval time.Duration
	metrics      *metrics.Metrics
}

func NewRelay(store Store, publisher Publisher, pollInterval time.Duration) *Relay {
	return &Relay{store: store, publisher: publisher, pollInterval: pollInterval}
}

// WithMetrics attaches a Prometheus metrics collector to the relay.
func (relay *Relay) WithMetrics(m *metrics.Metrics) *Relay {
	relay.metrics = m
	return relay
}

// Run polls until its context is cancelled.
func (relay *Relay) Run(ctx context.Context) error {
	ticker := time.NewTicker(relay.pollInterval)
	defer ticker.Stop()

	for {
		if err := relay.RunOnce(ctx); err != nil {
			return err
		}

		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

// RunOnce claims and publishes at most one outbox event.
func (relay *Relay) RunOnce(ctx context.Context) error {
	if reporter, ok := relay.store.(BacklogReporter); ok && relay.metrics != nil {
		if count, countErr := reporter.CountPending(ctx); countErr == nil {
			relay.metrics.OutboxBacklogRecords.Set(float64(count))
		}
	}

	event, found, err := relay.store.ClaimNext(ctx)
	if err != nil || !found {
		return err
	}

	if err := relay.publisher.Publish(ctx, event); err != nil {
		if recordErr := relay.store.RecordPublishFailure(ctx, event.ID); recordErr != nil {
			return fmt.Errorf("publish event %s: %w; record failure: %v", event.ID, err, recordErr)
		}
		return fmt.Errorf("publish event %s: %w", event.ID, err)
	}

	if err := relay.store.MarkPublished(ctx, event.ID); err != nil {
		return fmt.Errorf("mark event %s published: %w", event.ID, err)
	}

	return nil
}
