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
	t.Setenv("REDIS_ADDRESS", "")
	t.Setenv("REDIS_PASSWORD", "")
	t.Setenv("REDIS_TIMEOUT", "")
	t.Setenv("RATE_LIMIT_LOGIN_PER_MINUTE", "")
	t.Setenv("RATE_LIMIT_RESERVATION_PER_MINUTE", "")
	t.Setenv("CACHE_SOLD_OUT_TTL", "")

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
	if loaded.RedisAddress != "127.0.0.1:6379" {
		t.Fatalf("RedisAddress = %q, want 127.0.0.1:6379", loaded.RedisAddress)
	}
	if loaded.RedisPassword != "" {
		t.Fatalf("RedisPassword = %q, want empty", loaded.RedisPassword)
	}
	if loaded.RedisTimeout != 50*time.Millisecond {
		t.Fatalf("RedisTimeout = %s, want 50ms", loaded.RedisTimeout)
	}
	if loaded.RateLimitLoginPerMinute != 5 {
		t.Fatalf("RateLimitLoginPerMinute = %d, want 5", loaded.RateLimitLoginPerMinute)
	}
	if loaded.RateLimitReservationPerMinute != 10 {
		t.Fatalf("RateLimitReservationPerMinute = %d, want 10", loaded.RateLimitReservationPerMinute)
	}
	if loaded.CacheSoldOutTTL != 5*time.Second {
		t.Fatalf("CacheSoldOutTTL = %s, want 5s", loaded.CacheSoldOutTTL)
	}
}

func TestLoadRedisCustomConfiguration(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://orderflow:password@postgres:5432/orderflow?sslmode=disable")
	t.Setenv("REDIS_ADDRESS", "redis.internal:6380")
	t.Setenv("REDIS_PASSWORD", "secret-password")
	t.Setenv("REDIS_TIMEOUT", "2")
	t.Setenv("RATE_LIMIT_LOGIN_PER_MINUTE", "15")
	t.Setenv("RATE_LIMIT_RESERVATION_PER_MINUTE", "30")
	t.Setenv("CACHE_SOLD_OUT_TTL", "10")

	loaded, err := config.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if loaded.RedisAddress != "redis.internal:6380" {
		t.Fatalf("RedisAddress = %q, want redis.internal:6380", loaded.RedisAddress)
	}
	if loaded.RedisPassword != "secret-password" {
		t.Fatalf("RedisPassword = %q, want secret-password", loaded.RedisPassword)
	}
	if loaded.RedisTimeout != 2*time.Second {
		t.Fatalf("RedisTimeout = %s, want 2s", loaded.RedisTimeout)
	}
	if loaded.RateLimitLoginPerMinute != 15 {
		t.Fatalf("RateLimitLoginPerMinute = %d, want 15", loaded.RateLimitLoginPerMinute)
	}
	if loaded.RateLimitReservationPerMinute != 30 {
		t.Fatalf("RateLimitReservationPerMinute = %d, want 30", loaded.RateLimitReservationPerMinute)
	}
	if loaded.CacheSoldOutTTL != 10*time.Second {
		t.Fatalf("CacheSoldOutTTL = %s, want 10s", loaded.CacheSoldOutTTL)
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

func TestLoadFeatureFlagsDefaults(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://orderflow:password@postgres:5432/orderflow?sslmode=disable")
	t.Setenv("ORDERFLOW_CACHE_ENABLED", "")
	t.Setenv("ORDERFLOW_SOLDOUT_FASTPATH", "")
	t.Setenv("ORDERFLOW_RATE_LIMIT_ENABLED", "")
	t.Setenv("LOAD_TEST_MODE", "")

	loaded, err := config.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if !loaded.CacheEnabled {
		t.Errorf("CacheEnabled = false, want default true")
	}
	if !loaded.SoldOutFastPath {
		t.Errorf("SoldOutFastPath = false, want default true")
	}
	if !loaded.RateLimitEnabled {
		t.Errorf("RateLimitEnabled = false, want default true")
	}
	if loaded.LoadTestMode {
		t.Errorf("LoadTestMode = true, want default false")
	}
}

func TestLoadFeatureFlagsCustom(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://orderflow:password@postgres:5432/orderflow?sslmode=disable")
	t.Setenv("ORDERFLOW_CACHE_ENABLED", "false")
	t.Setenv("ORDERFLOW_SOLDOUT_FASTPATH", "false")
	t.Setenv("ORDERFLOW_RATE_LIMIT_ENABLED", "false")
	t.Setenv("LOAD_TEST_MODE", "true")

	loaded, err := config.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if loaded.CacheEnabled {
		t.Errorf("CacheEnabled = true, want false")
	}
	if loaded.SoldOutFastPath {
		t.Errorf("SoldOutFastPath = true, want false")
	}
	if loaded.RateLimitEnabled {
		t.Errorf("RateLimitEnabled = true, want false")
	}
	if !loaded.LoadTestMode {
		t.Errorf("LoadTestMode = false, want true")
	}
}

func TestLoadRejectsLoadTestModeInProduction(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://orderflow:password@postgres:5432/orderflow?sslmode=disable")
	t.Setenv("APP_ENV", "production")
	t.Setenv("LOAD_TEST_MODE", "true")

	_, err := config.Load()
	if err == nil || !strings.Contains(err.Error(), "LOAD_TEST_MODE") {
		t.Fatalf("Load() error = %v, want error rejecting LOAD_TEST_MODE in production", err)
	}
}
