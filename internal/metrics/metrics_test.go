package metrics_test

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/theRiddler9/OrderFlow/internal/metrics"
)

func TestMetricsRegistrationAndCollection(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := metrics.New(reg)
	if m == nil {
		t.Fatal("expected non-nil Metrics instance")
	}

	// 1. HTTP metrics
	m.HTTPRequestsTotal.WithLabelValues("GET", "/healthz", "200").Inc()
	m.HTTPRequestDuration.WithLabelValues("GET", "/healthz").Observe(0.012)
	m.RateLimitRejectedTotal.WithLabelValues("/v1/auth/login").Inc()

	// 2. Cache metrics
	m.RedisCacheHitsTotal.WithLabelValues("sold_out").Inc()
	m.RedisCacheMissesTotal.WithLabelValues("sold_out").Inc()
	m.RedisFailOpenTotal.WithLabelValues("ratelimit").Inc()

	// 3. Outbox metrics
	m.OutboxBacklogRecords.Set(42)
	m.OutboxOldestRowAgeSeconds.Set(3.5)

	// 4. Kafka metrics
	m.KafkaConsumerLag.WithLabelValues("reservation-events", "0").Set(5)

	// 5. Worker metrics
	m.WorkerEventsProcessedTotal.WithLabelValues("reservation.held.v1", "confirmed").Inc()
	m.WorkerEventDurationSeconds.WithLabelValues("reservation.held.v1").Observe(0.045)
	m.WorkerRetriesTotal.WithLabelValues("reservation.held.v1").Inc()
	m.WorkerDLQEventsTotal.WithLabelValues("reservation.held.v1", "poison_payload").Inc()
	m.SagaLatencySeconds.WithLabelValues("confirmed").Observe(0.125)

	// Gather metrics from isolated registry
	metricFamilies, err := reg.Gather()
	if err != nil {
		t.Fatalf("Gather() error = %v", err)
	}

	expectedMetrics := map[string]bool{
		"orderflow_http_requests_total":             false,
		"orderflow_http_request_duration_seconds":   false,
		"orderflow_ratelimit_rejected_total":        false,
		"orderflow_redis_cache_hits_total":          false,
		"orderflow_redis_cache_misses_total":        false,
		"orderflow_redis_failopen_total":            false,
		"orderflow_outbox_backlog_records":          false,
		"orderflow_outbox_oldest_row_age_seconds":   false,
		"orderflow_kafka_consumer_lag":              false,
		"orderflow_worker_events_processed_total":   false,
		"orderflow_worker_event_duration_seconds":   false,
		"orderflow_worker_retries_total":            false,
		"orderflow_worker_dlq_events_total":         false,
		"orderflow_saga_duration_seconds":           false,
	}

	for _, mf := range metricFamilies {
		name := mf.GetName()
		if _, exists := expectedMetrics[name]; exists {
			expectedMetrics[name] = true
		}
	}

	for name, found := range expectedMetrics {
		if !found {
			t.Errorf("expected metric %q to be registered and gathered, but was missing", name)
		}
	}
}
