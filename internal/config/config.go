package config

import (
	"fmt"
	"net/url"
	"os"
	"strconv"
	"time"
)

// Config contains process configuration supplied by the environment.
type Config struct {
	Environment     string
	HTTPAddress     string
	DatabaseURL     string
	DatabaseTimeout time.Duration
	ReservationHold time.Duration
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

	config := Config{
		Environment:     valueOrDefault("APP_ENV", "development"),
		HTTPAddress:     valueOrDefault("HTTP_ADDRESS", ":8080"),
		DatabaseURL:     os.Getenv("DATABASE_URL"),
		DatabaseTimeout: timeout,
		ReservationHold: holdDuration,
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
