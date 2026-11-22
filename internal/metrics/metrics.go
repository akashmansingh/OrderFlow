package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
)

// Metrics holds all Prometheus metrics registered for OrderFlow.
type Metrics struct {
	Registry prometheus.Registerer

	// HTTP metrics
	HTTPRequestsTotal      *prometheus.CounterVec
	HTTPRequestDuration    *prometheus.HistogramVec
	RateLimitRejectedTotal *prometheus.CounterVec

	// Cache metrics
	RedisCacheHitsTotal   *prometheus.CounterVec
	RedisCacheMissesTotal *prometheus.CounterVec
	RedisFailOpenTotal    *prometheus.CounterVec

	// Outbox & Worker metrics
	OutboxBacklogRecords       prometheus.Gauge
	WorkerEventsProcessedTotal *prometheus.CounterVec
	WorkerEventDurationSeconds *prometheus.HistogramVec
	WorkerRetriesTotal         *prometheus.CounterVec
	WorkerDLQEventsTotal       *prometheus.CounterVec
}

// New creates and registers OrderFlow metrics against the provided Prometheus registerer.
// If registerer is nil, prometheus.DefaultRegisterer is used.
func New(registerer prometheus.Registerer) *Metrics {
	if registerer == nil {
		registerer = prometheus.DefaultRegisterer
	}

	m := &Metrics{
		Registry: registerer,

		HTTPRequestsTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: "orderflow",
				Subsystem: "http",
				Name:      "requests_total",
				Help:      "Total count of HTTP requests handled by the API server.",
			},
			[]string{"method", "path", "status"},
		),

		HTTPRequestDuration: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Namespace: "orderflow",
				Subsystem: "http",
				Name:      "request_duration_seconds",
				Help:      "Histogram of HTTP request latencies in seconds.",
				Buckets:   []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5},
			},
			[]string{"method", "path"},
		),

		RateLimitRejectedTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: "orderflow",
				Name:      "ratelimit_rejected_total",
				Help:      "Total number of requests rejected due to rate limiting.",
			},
			[]string{"endpoint"},
		),

		RedisCacheHitsTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: "orderflow",
				Subsystem: "redis",
				Name:      "cache_hits_total",
				Help:      "Total number of Redis cache hits.",
			},
			[]string{"cache"},
		),

		RedisCacheMissesTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: "orderflow",
				Subsystem: "redis",
				Name:      "cache_misses_total",
				Help:      "Total number of Redis cache misses.",
			},
			[]string{"cache"},
		),

		RedisFailOpenTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: "orderflow",
				Subsystem: "redis",
				Name:      "failopen_total",
				Help:      "Total count of times Redis operations failed open due to errors or timeouts.",
			},
			[]string{"operation"},
		),

		OutboxBacklogRecords: prometheus.NewGauge(
			prometheus.GaugeOpts{
				Namespace: "orderflow",
				Subsystem: "outbox",
				Name:      "backlog_records",
				Help:      "Current number of pending, unpublished records in the transactional outbox table.",
			},
		),

		WorkerEventsProcessedTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: "orderflow",
				Subsystem: "worker",
				Name:      "events_processed_total",
				Help:      "Total count of workflow events processed by the worker.",
			},
			[]string{"event_type", "outcome"},
		),

		WorkerEventDurationSeconds: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Namespace: "orderflow",
				Subsystem: "worker",
				Name:      "event_duration_seconds",
				Help:      "Histogram of event processing durations in seconds.",
				Buckets:   []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5},
			},
			[]string{"event_type"},
		),

		WorkerRetriesTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: "orderflow",
				Subsystem: "worker",
				Name:      "retries_total",
				Help:      "Total number of event processing retries.",
			},
			[]string{"event_type"},
		),

		WorkerDLQEventsTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: "orderflow",
				Subsystem: "worker",
				Name:      "dlq_events_total",
				Help:      "Total number of poison or unrecoverable events forwarded to the dead-letter queue.",
			},
			[]string{"event_type"},
		),
	}

	// Register all collectors with the registerer
	registerer.MustRegister(
		m.HTTPRequestsTotal,
		m.HTTPRequestDuration,
		m.RateLimitRejectedTotal,
		m.RedisCacheHitsTotal,
		m.RedisCacheMissesTotal,
		m.RedisFailOpenTotal,
		m.OutboxBacklogRecords,
		m.WorkerEventsProcessedTotal,
		m.WorkerEventDurationSeconds,
		m.WorkerRetriesTotal,
		m.WorkerDLQEventsTotal,
	)

	return m
}
