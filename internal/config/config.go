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
	Environment            string
	HTTPAddress            string
	DatabaseURL            string
	DatabaseTimeout        time.Duration
	ReservationHold        time.Duration
	KafkaBrokers           []string
	KafkaTopic             string
	KafkaGroupID           string
	WorkerPollInterval     time.Duration
	SimulatedPaymentResult string
}

// Load reads and validates the configuration required by the reservation API.
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

	config := Config{
		Environment:            valueOrDefault("APP_ENV", "development"),
		HTTPAddress:            valueOrDefault("HTTP_ADDRESS", ":8080"),
		DatabaseURL:            os.Getenv("DATABASE_URL"),
		DatabaseTimeout:        timeout,
		ReservationHold:        holdDuration,
		KafkaBrokers:           brokers,
		KafkaTopic:             valueOrDefault("KAFKA_TOPIC", "reservation-events"),
		KafkaGroupID:           valueOrDefault("KAFKA_GROUP_ID", "orderflow-workflow-worker"),
		WorkerPollInterval:     pollInterval,
		SimulatedPaymentResult: paymentResult,
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
