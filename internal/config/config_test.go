package config_test

import (
	"strings"
	"testing"
	"time"

	"github.com/theRiddler9/OrderFlow/internal/config"
)

func TestLoadDefaultsWorkerConfiguration(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://orderflow:password@postgres:5432/orderflow?sslmode=disable")
	t.Setenv("KAFKA_BROKERS", "")
	t.Setenv("KAFKA_TOPIC", "")
	t.Setenv("KAFKA_GROUP_ID", "")
	t.Setenv("WORKER_POLL_SECONDS", "")
	t.Setenv("SIMULATED_PAYMENT_RESULT", "")

	loaded, err := config.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got, want := loaded.KafkaBrokers, []string{"redpanda:9092"}; len(got) != len(want) || got[0] != want[0] {
		t.Fatalf("KafkaBrokers = %v, want %v", got, want)
	}
	if loaded.KafkaTopic != "reservation-events" {
		t.Fatalf("KafkaTopic = %q", loaded.KafkaTopic)
	}
	if loaded.KafkaGroupID != "orderflow-workflow-worker" {
		t.Fatalf("KafkaGroupID = %q", loaded.KafkaGroupID)
	}
	if loaded.WorkerPollInterval != time.Second {
		t.Fatalf("WorkerPollInterval = %s, want 1s", loaded.WorkerPollInterval)
	}
	if loaded.SimulatedPaymentResult != "approve" {
		t.Fatalf("SimulatedPaymentResult = %q, want approve", loaded.SimulatedPaymentResult)
	}
}

func TestLoadRejectsUnsupportedSimulatedPaymentResult(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://orderflow:password@postgres:5432/orderflow?sslmode=disable")
	t.Setenv("SIMULATED_PAYMENT_RESULT", "maybe")

	_, err := config.Load()
	if err == nil || !strings.Contains(err.Error(), "SIMULATED_PAYMENT_RESULT") {
		t.Fatalf("Load() error = %v, want payment result validation error", err)
	}
}
