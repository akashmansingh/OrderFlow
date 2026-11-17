package messaging

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/theRiddler9/OrderFlow/internal/events"
	"github.com/twmb/franz-go/pkg/kgo"
)

// NewProducer creates a Kafka-compatible client for Redpanda.
func NewProducer(brokers []string) (*kgo.Client, error) {
	if len(brokers) == 0 {
		return nil, fmt.Errorf("at least one Kafka broker is required")
	}
	return kgo.NewClient(
		kgo.SeedBrokers(brokers...),
		kgo.AllowAutoTopicCreation(),
	)
}

// Publisher sends event envelopes to one topic.
type Publisher struct {
	client *kgo.Client
	topic  string
}

func NewPublisher(client *kgo.Client, topic string) *Publisher {
	return &Publisher{client: client, topic: topic}
}

func (publisher *Publisher) Publish(ctx context.Context, event events.Envelope) error {
	value, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("encode event %s: %w", event.ID, err)
	}
	result := publisher.client.ProduceSync(ctx, &kgo.Record{
		Topic: publisher.topic,
		Key:   []byte(event.ID),
		Value: value,
		Headers: []kgo.RecordHeader{
			{Key: "event_id", Value: []byte(event.ID)},
			{Key: "event_type", Value: []byte(event.Type)},
		},
	})
	if err := result.FirstErr(); err != nil {
		return fmt.Errorf("produce event %s: %w", event.ID, err)
	}
	return nil
}
