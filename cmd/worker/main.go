package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/theRiddler9/OrderFlow/internal/cache"
	"github.com/theRiddler9/OrderFlow/internal/config"
	"github.com/theRiddler9/OrderFlow/internal/database"
	"github.com/theRiddler9/OrderFlow/internal/events"
	"github.com/theRiddler9/OrderFlow/internal/messaging"
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
	publisher := messaging.NewPublisher(producerClient, configuration.KafkaTopic)
	relay := outbox.NewRelay(outbox.NewPostgresStore(databasePool, 30*time.Second), publisher, configuration.WorkerPollInterval)
	processor := workflow.NewProcessor(databasePool, configuration.SimulatedPaymentResult)
	expiry := workflow.NewExpiry(databasePool)

	if configuration.RedisAddress != "" {
		rc, err := cache.New(configuration.RedisAddress, configuration.RedisPassword, configuration.RedisTimeout, logger)
		if err != nil {
			logger.Warn("redis connection unavailable, worker cache invalidation disabled", "error", err)
		} else {
			defer rc.Close()
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

	for ctx.Err() == nil {
		fetches := consumer.PollFetches(ctx)
		if errs := fetches.Errors(); len(errs) > 0 {
			return fmt.Errorf("poll Kafka: %w", errs[0].Err)
		}
		fetches.EachRecord(func(record *kgo.Record) {
			if err := handleRecord(ctx, processor, consumer, record); err != nil {
				logger.Error("handle Kafka record", "error", err)
			}
		})
	}
	return nil
}

func handleRecord(ctx context.Context, handler eventHandler, committer offsetCommitter, record *kgo.Record) error {
	event, err := events.ParseEnvelope(record.Value)
	if err != nil {
		return err
	}
	if err := handler.Handle(ctx, event); err != nil {
		return err
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
