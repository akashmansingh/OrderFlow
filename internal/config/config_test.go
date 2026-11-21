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
	t.Setenv("JWT_ACCESS_TOKEN_MINUTES", "")
	t.Setenv("JWT_ISSUER", "")
	t.Setenv("JWT_AUDIENCE", "")

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
	if loaded.JWTAccessTokenMinutes != 15 {
		t.Fatalf("JWTAccessTokenMinutes = %d, want 15", loaded.JWTAccessTokenMinutes)
	}
	if loaded.JWTIssuer != "orderflow" {
		t.Fatalf("JWTIssuer = %q, want orderflow", loaded.JWTIssuer)
	}
	if loaded.JWTAudience != "orderflow-api" {
		t.Fatalf("JWTAudience = %q, want orderflow-api", loaded.JWTAudience)
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

func TestLoadAPIRequiresJWTSecret(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://orderflow:password@postgres:5432/orderflow?sslmode=disable")
	t.Setenv("JWT_SECRET", "")

	_, err := config.LoadAPI()
	if err == nil || !strings.Contains(err.Error(), "JWT_SECRET") {
		t.Fatalf("LoadAPI() error = %v, want JWT secret validation error", err)
	}
}

func TestLoadAPIRejectsShortJWTSecret(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://orderflow:password@postgres:5432/orderflow?sslmode=disable")
	t.Setenv("JWT_SECRET", "too-short")

	_, err := config.LoadAPI()
	if err == nil || !strings.Contains(err.Error(), "JWT_SECRET") {
		t.Fatalf("LoadAPI() error = %v, want JWT secret validation error", err)
	}
}

func TestLoadAPIReadsJWTSettings(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://orderflow:password@postgres:5432/orderflow?sslmode=disable")
	t.Setenv("JWT_SECRET", "a-local-development-secret-that-is-long-enough")
	t.Setenv("JWT_ACCESS_TOKEN_MINUTES", "30")
	t.Setenv("JWT_ISSUER", "test-issuer")
	t.Setenv("JWT_AUDIENCE", "test-audience")

	loaded, err := config.LoadAPI()
	if err != nil {
		t.Fatalf("LoadAPI() error = %v", err)
	}
	if loaded.JWTAccessTokenMinutes != 30 {
		t.Fatalf("JWTAccessTokenMinutes = %d, want 30", loaded.JWTAccessTokenMinutes)
	}
	if loaded.JWTIssuer != "test-issuer" || loaded.JWTAudience != "test-audience" {
		t.Fatalf("JWT issuer/audience = %q/%q", loaded.JWTIssuer, loaded.JWTAudience)
	}
}
