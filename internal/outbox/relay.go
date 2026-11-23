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

// BacklogReporter reports the number and oldest age of pending outbox records.
type BacklogReporter interface {
	ReportBacklog(context.Context) (int64, float64, error)
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

// Run polls until its context is cancelled. It rapidly drains all available outbox
// records and only sleeps on pollInterval when the queue is empty.
func (relay *Relay) Run(ctx context.Context) error {
	ticker := time.NewTicker(relay.pollInterval)
	defer ticker.Stop()

	for {
		for {
			claimed, err := relay.claimAndPublish(ctx)
			if err != nil {
				return err
			}
			if !claimed {
				break
			}
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
	_, err := relay.claimAndPublish(ctx)
	return err
}

func (relay *Relay) claimAndPublish(ctx context.Context) (bool, error) {
	if reporter, ok := relay.store.(BacklogReporter); ok && relay.metrics != nil {
		if count, oldestAge, countErr := reporter.ReportBacklog(ctx); countErr == nil {
			relay.metrics.OutboxBacklogRecords.Set(float64(count))
			relay.metrics.OutboxOldestRowAgeSeconds.Set(oldestAge)
		}
	}

	event, found, err := relay.store.ClaimNext(ctx)
	if err != nil || !found {
		return false, err
	}

	if err := relay.publisher.Publish(ctx, event); err != nil {
		if recordErr := relay.store.RecordPublishFailure(ctx, event.ID); recordErr != nil {
			return false, fmt.Errorf("publish event %s: %w; record failure: %v", event.ID, err, recordErr)
		}
		return false, fmt.Errorf("publish event %s: %w", event.ID, err)
	}

	if err := relay.store.MarkPublished(ctx, event.ID); err != nil {
		return false, fmt.Errorf("mark event %s published: %w", event.ID, err)
	}

	return true, nil
}
