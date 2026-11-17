package messaging_test

import (
	"testing"

	"github.com/theRiddler9/OrderFlow/internal/messaging"
)

func TestNewProducerUsesConfiguredBrokers(t *testing.T) {
	producer, err := messaging.NewProducer([]string{"redpanda:9092"})
	if err != nil {
		t.Fatalf("NewProducer() error = %v", err)
	}
	if producer == nil {
		t.Fatal("NewProducer() = nil")
	}
	producer.Close()
}

func TestNewProducerRejectsEmptyBrokerList(t *testing.T) {
	producer, err := messaging.NewProducer(nil)
	if err == nil {
		producer.Close()
		t.Fatal("NewProducer() error = nil, want error")
	}
}
