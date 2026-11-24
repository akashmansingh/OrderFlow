package config

import (
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config contains process configuration supplied by the environment.
type Config struct {
	Environment                   string
	HTTPAddress                   string
	DatabaseURL                   string
	DatabaseTimeout               time.Duration
	ReservationHold               time.Duration
	KafkaBrokers                  []string
	KafkaTopic                    string
	KafkaGroupID                  string
	WorkerPollInterval            time.Duration
	SimulatedPaymentResult        string
	JWTSecret                     string
	JWTAccessTokenMinutes         int
	JWTIssuer                     string
	JWTAudience                   string
	RedisAddress                  string
	RedisPassword                 string
	RedisTimeout                  time.Duration
	RateLimitLoginPerMinute       int
	RateLimitReservationPerMinute int
	CacheSoldOutTTL               time.Duration
	CacheEnabled                  bool
	SoldOutFastPath               bool
	RateLimitEnabled              bool
	LoadTestMode                  bool
	OTLPEndpoint                  string
	TracingEnabled                bool
	AutoConfirmHolds              bool
	SimulatedPaymentDelay         time.Duration
}

// Load reads configuration shared by the API and worker.
func Load() (Config, error) {
	timeout, err := durationFromEnv("DATABASE_TIMEOUT", 5*time.Second)
	if err != nil {
		return Config{}, err
	}
	holdDuration, err := durationFromEnv("RESERVATION_HOLD_SECONDS", 10*time.Minute)
	if err != nil {
		return Config{}, err
	}
	pollInterval, err := durationFromEnv("WORKER_POLL_SECONDS", time.Second)
	if err != nil {
		return Config{}, err
	}
	brokers, err := brokersFromEnv()
	if err != nil {
		return Config{}, err
	}
	paymentResult, err := paymentResultFromEnv()
	if err != nil {
		return Config{}, err
	}
	accessTokenMinutes, err := positiveIntFromEnv("JWT_ACCESS_TOKEN_MINUTES", 15)
	if err != nil {
		return Config{}, err
	}
	redisTimeout, err := redisTimeoutFromEnv(50 * time.Millisecond)
	if err != nil {
		return Config{}, err
	}
	rateLimitLogin, err := positiveIntFromEnv("RATE_LIMIT_LOGIN_PER_MINUTE", 5)
	if err != nil {
		return Config{}, err
	}
	rateLimitReservation, err := positiveIntFromEnv("RATE_LIMIT_RESERVATION_PER_MINUTE", 10)
	if err != nil {
		return Config{}, err
	}
	soldOutTTL, err := durationFromEnv("CACHE_SOLD_OUT_TTL", 5*time.Second)
	if err != nil {
		return Config{}, err
	}
	cacheEnabled, err := boolFromEnv("ORDERFLOW_CACHE_ENABLED", true)
	if err != nil {
		return Config{}, err
	}
	soldOutFastPath, err := boolFromEnv("ORDERFLOW_SOLDOUT_FASTPATH", true)
	if err != nil {
		return Config{}, err
	}
	rateLimitEnabled, err := boolFromEnv("ORDERFLOW_RATE_LIMIT_ENABLED", true)
	if err != nil {
		return Config{}, err
	}
	loadTestMode, err := boolFromEnv("LOAD_TEST_MODE", false)
	if err != nil {
		return Config{}, err
	}
	tracingEnabled, err := boolFromEnv("ORDERFLOW_TRACING_ENABLED", true)
	if err != nil {
		return Config{}, err
	}
	autoConfirmHolds, err := boolFromEnv("AUTO_CONFIRM_HOLDS", false)
	if err != nil {
		return Config{}, err
	}
	simulatedPaymentDelay, err := durationFromEnv("SIMULATED_PAYMENT_DELAY", 0)
	if err != nil {
		return Config{}, err
	}

	config := Config{
		Environment:                   valueOrDefault("APP_ENV", "development"),
		HTTPAddress:                   valueOrDefault("HTTP_ADDRESS", ":8080"),
		DatabaseURL:                   os.Getenv("DATABASE_URL"),
		DatabaseTimeout:               timeout,
		ReservationHold:               holdDuration,
		KafkaBrokers:                  brokers,
		KafkaTopic:                    valueOrDefault("KAFKA_TOPIC", "reservation-events"),
		KafkaGroupID:                  valueOrDefault("KAFKA_GROUP_ID", "orderflow-workflow-worker"),
		WorkerPollInterval:            pollInterval,
		SimulatedPaymentResult:        paymentResult,
		JWTSecret:                     os.Getenv("JWT_SECRET"),
		JWTAccessTokenMinutes:         accessTokenMinutes,
		JWTIssuer:                     valueOrDefault("JWT_ISSUER", "orderflow"),
		JWTAudience:                   valueOrDefault("JWT_AUDIENCE", "orderflow-api"),
		RedisAddress:                  valueOrDefault("REDIS_ADDRESS", "127.0.0.1:6379"),
		RedisPassword:                 os.Getenv("REDIS_PASSWORD"),
		RedisTimeout:                  redisTimeout,
		RateLimitLoginPerMinute:       rateLimitLogin,
		RateLimitReservationPerMinute: rateLimitReservation,
		CacheSoldOutTTL:               soldOutTTL,
		CacheEnabled:                  cacheEnabled,
		SoldOutFastPath:               soldOutFastPath,
		RateLimitEnabled:              rateLimitEnabled,
		LoadTestMode:                  loadTestMode,
		OTLPEndpoint:                  strings.TrimSpace(os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT")),
		TracingEnabled:                tracingEnabled,
		AutoConfirmHolds:              autoConfirmHolds,
		SimulatedPaymentDelay:         simulatedPaymentDelay,
	}

	if config.Environment == "production" && config.LoadTestMode {
		return Config{}, fmt.Errorf("cannot enable LOAD_TEST_MODE in production environment")
	}

	if config.DatabaseURL == "" {
		return Config{}, fmt.Errorf("DATABASE_URL is required")
	}

	parsedURL, err := url.Parse(config.DatabaseURL)
	if err != nil || parsedURL.Scheme == "" || parsedURL.Host == "" {
		return Config{}, fmt.Errorf("DATABASE_URL must be a valid connection URL")
	}

	return config, nil
}

// LoadAPI loads shared configuration and requires the API-only JWT signing settings.
// The worker does not need the signing secret, so it continues to use Load.
func LoadAPI() (Config, error) {
	config, err := Load()
	if err != nil {
		return Config{}, err
	}
	if len(config.JWTSecret) < 32 {
		return Config{}, fmt.Errorf("JWT_SECRET must be at least 32 bytes")
	}
	if config.JWTIssuer == "" || config.JWTAudience == "" {
		return Config{}, fmt.Errorf("JWT_ISSUER and JWT_AUDIENCE are required")
	}
	if config.JWTAccessTokenMinutes <= 0 {
		return Config{}, fmt.Errorf("JWT_ACCESS_TOKEN_MINUTES must be a positive number of minutes")
	}
	return config, nil
}

func brokersFromEnv() ([]string, error) {
	value := valueOrDefault("KAFKA_BROKERS", "redpanda:9092")
	parts := strings.Split(value, ",")
	brokers := make([]string, 0, len(parts))
	for _, part := range parts {
		broker := strings.TrimSpace(part)
		if broker == "" {
			return nil, fmt.Errorf("KAFKA_BROKERS must contain non-empty broker addresses")
		}
		brokers = append(brokers, broker)
	}
	return brokers, nil
}

func paymentResultFromEnv() (string, error) {
	result := strings.ToLower(strings.TrimSpace(valueOrDefault("SIMULATED_PAYMENT_RESULT", "approve")))
	if result != "approve" && result != "reject" {
		return "", fmt.Errorf("SIMULATED_PAYMENT_RESULT must be approve or reject")
	}
	return result, nil
}

func positiveIntFromEnv(key string, fallback int) (int, error) {
	value := os.Getenv(key)
	if value == "" {
		return fallback, nil
	}

	parsed, err := strconv.Atoi(value)
	if err != nil || parsed <= 0 {
		return 0, fmt.Errorf("%s must be a positive integer", key)
	}
	return parsed, nil
}

func redisTimeoutFromEnv(fallback time.Duration) (time.Duration, error) {
	if value := os.Getenv("REDIS_TIMEOUT_MS"); value != "" {
		ms, err := strconv.Atoi(value)
		if err != nil || ms <= 0 {
			return 0, fmt.Errorf("REDIS_TIMEOUT_MS must be a positive number of milliseconds")
		}
		return time.Duration(ms) * time.Millisecond, nil
	}

	if value := os.Getenv("REDIS_TIMEOUT"); value != "" {
		seconds, err := strconv.Atoi(value)
		if err != nil || seconds <= 0 {
			return 0, fmt.Errorf("REDIS_TIMEOUT must be a positive number of seconds")
		}
		return time.Duration(seconds) * time.Second, nil
	}

	return fallback, nil
}

func durationFromEnv(key string, fallback time.Duration) (time.Duration, error) {
	value := os.Getenv(key)
	if value == "" {
		return fallback, nil
	}

	seconds, err := strconv.Atoi(value)
	if err != nil || seconds <= 0 {
		return 0, fmt.Errorf("%s must be a positive number of seconds", key)
	}

	return time.Duration(seconds) * time.Second, nil
}

func valueOrDefault(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}

	return fallback
}

func boolFromEnv(key string, fallback bool) (bool, error) {
	val := strings.TrimSpace(os.Getenv(key))
	if val == "" {
		return fallback, nil
	}
	parsed, err := strconv.ParseBool(val)
	if err != nil {
		return false, fmt.Errorf("%s must be a valid boolean: %w", key, err)
	}
	return parsed, nil
}
