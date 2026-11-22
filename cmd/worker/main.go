package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/theRiddler9/OrderFlow/internal/cache"
	"github.com/theRiddler9/OrderFlow/internal/config"
	"github.com/theRiddler9/OrderFlow/internal/database"
	"github.com/theRiddler9/OrderFlow/internal/events"
	"github.com/theRiddler9/OrderFlow/internal/messaging"
	"github.com/theRiddler9/OrderFlow/internal/metrics"
	"github.com/theRiddler9/OrderFlow/internal/outbox"
	"github.com/theRiddler9/OrderFlow/internal/workflow"
	"github.com/twmb/franz-go/pkg/kgo"
)

type eventHandler interface {
	Handle(context.Context, events.Envelope) error
}

type offsetCommitter interface {
	CommitRecords(context.Context, ...*kgo.Record) error
}

type dlqPublisher interface {
	PublishDLQ(ctx context.Context, originalRecord []byte, reason string, retryCount int) error
}

type KafkaDLQPublisher struct {
	client *kgo.Client
	topic  string
}

func NewKafkaDLQPublisher(client *kgo.Client, topic string) *KafkaDLQPublisher {
	return &KafkaDLQPublisher{client: client, topic: topic}
}

func (p *KafkaDLQPublisher) PublishDLQ(ctx context.Context, originalRecord []byte, reason string, retryCount int) error {
	envelope, err := workflow.FormatDLQEnvelope(originalRecord, reason, retryCount)
	if err != nil {
		return err
	}
	record := &kgo.Record{
		Topic: p.topic,
		Value: envelope,
	}
	return p.client.ProduceSync(ctx, record).FirstErr()
}

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(logger); err != nil {
		logger.Error("worker stopped", "error", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	configuration, err := config.Load()
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	databasePool, err := database.Connect(ctx, configuration.DatabaseURL, configuration.DatabaseTimeout)
	if err != nil {
		return err
	}
	defer databasePool.Close()

	producerClient, err := messaging.NewProducer(configuration.KafkaBrokers)
	if err != nil {
		return err
	}
	defer producerClient.Close()
	metricsCollector := metrics.New(nil)
	metricsCollector.RegisterPoolStats(databasePool, "worker")

	publisher := messaging.NewPublisher(producerClient, configuration.KafkaTopic)
	relay := outbox.NewRelay(outbox.NewPostgresStore(databasePool, 30*time.Second), publisher, configuration.WorkerPollInterval).WithMetrics(metricsCollector)
	processor := workflow.NewProcessor(databasePool, configuration.SimulatedPaymentResult).WithMetrics(metricsCollector)
	expiry := workflow.NewExpiry(databasePool).WithMetrics(metricsCollector)

	if configuration.RedisAddress != "" {
		rc, err := cache.New(configuration.RedisAddress, configuration.RedisPassword, configuration.RedisTimeout, logger)
		if err != nil {
			logger.Warn("redis connection unavailable, worker cache invalidation disabled", "error", err)
		} else {
			defer rc.Close()
			rc.WithMetrics(metricsCollector)
			processor.WithCache(rc)
			expiry.WithCache(rc)
		}
	}

	consumer, err := kgo.NewClient(
		kgo.SeedBrokers(configuration.KafkaBrokers...),
		kgo.ConsumerGroup(configuration.KafkaGroupID),
		kgo.ConsumeTopics(configuration.KafkaTopic),
		kgo.DisableAutoCommit(),
	)
	if err != nil {
		return fmt.Errorf("create Kafka consumer: %w", err)
	}
	defer consumer.Close()

	go runRelay(ctx, logger, relay)
	go runExpiry(ctx, logger, expiry, configuration.WorkerPollInterval)
	logger.Info("worker started", "topic", configuration.KafkaTopic, "group", configuration.KafkaGroupID)

	dlqPublisher := NewKafkaDLQPublisher(producerClient, configuration.KafkaTopic+"-dlq")

	for ctx.Err() == nil {
		fetches := consumer.PollFetches(ctx)
		if errs := fetches.Errors(); len(errs) > 0 {
			return fmt.Errorf("poll Kafka: %w", errs[0].Err)
		}
		fetches.EachPartition(func(p kgo.FetchTopicPartition) {
			lag := int64(0)
			if len(p.Records) > 0 {
				lastOffset := p.Records[len(p.Records)-1].Offset
				lag = p.HighWatermark - lastOffset - 1
				if lag < 0 {
					lag = 0
				}
			}
			if metricsCollector != nil {
				metricsCollector.KafkaConsumerLag.WithLabelValues(p.Topic, strconv.Itoa(int(p.Partition))).Set(float64(lag))
			}
		})
		fetches.EachRecord(func(record *kgo.Record) {
			if err := handleRecord(ctx, processor, consumer, dlqPublisher, metricsCollector, logger, record); err != nil {
				logger.Error("handle Kafka record", "error", err)
			}
		})
	}
	return nil
}

func handleRecord(
	ctx context.Context,
	handler eventHandler,
	committer offsetCommitter,
	dlq dlqPublisher,
	m *metrics.Metrics,
	logger *slog.Logger,
	record *kgo.Record,
) error {
	event, err := events.ParseEnvelope(record.Value)
	if err != nil {
		if logger != nil {
			logger.Warn("poison record detected, routing to DLQ", "error", err)
		}
		if dlq != nil {
			_ = dlq.PublishDLQ(ctx, record.Value, err.Error(), 0)
		}
		if m != nil {
			m.WorkerDLQEventsTotal.WithLabelValues("poison", "unparseable_envelope").Inc()
		}
		return committer.CommitRecords(ctx, record)
	}

	const maxRetries = 3
	var lastErr error
	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			if m != nil {
				m.WorkerRetriesTotal.WithLabelValues(event.Type).Inc()
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(time.Duration(attempt*10) * time.Millisecond):
			}
		}

		err = handler.Handle(ctx, event)
		if err == nil {
			return committer.CommitRecords(ctx, record)
		}

		lastErr = err
		if workflow.IsPoisonError(err) {
			break
		}
	}

	reason := "max_retries_exceeded"
	if workflow.IsPoisonError(lastErr) {
		reason = "poison_payload"
	}

	if logger != nil {
		logger.Error("message handling failed, forwarding to DLQ", "event_id", event.ID, "reason", reason, "error", lastErr)
	}
	if dlq != nil {
		_ = dlq.PublishDLQ(ctx, record.Value, lastErr.Error(), maxRetries)
	}
	if m != nil {
		m.WorkerDLQEventsTotal.WithLabelValues(event.Type, reason).Inc()
	}

	return committer.CommitRecords(ctx, record)
}

func runRelay(ctx context.Context, logger *slog.Logger, relay *outbox.Relay) {
	if err := relay.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
		logger.Error("outbox relay stopped", "error", err)
	}
}

func runExpiry(ctx context.Context, logger *slog.Logger, expiry *workflow.Expiry, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		if err := expiry.RunOnce(ctx); err != nil && !errors.Is(err, context.Canceled) {
			logger.Error("reservation expiry failed", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
