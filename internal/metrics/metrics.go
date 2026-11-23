package metrics

import (
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
)

var defaultLatencyBuckets = []float64{0.001, 0.002, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5}

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

	// Outbox & Broker metrics
	OutboxBacklogRecords      prometheus.Gauge
	OutboxOldestRowAgeSeconds prometheus.Gauge
	KafkaConsumerLag          *prometheus.GaugeVec

	// Worker & Saga metrics
	WorkerEventsProcessedTotal *prometheus.CounterVec
	WorkerEventDurationSeconds *prometheus.HistogramVec
	WorkerRetriesTotal         *prometheus.CounterVec
	WorkerDLQEventsTotal       *prometheus.CounterVec
	SagaLatencySeconds         *prometheus.HistogramVec
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
				Buckets:   defaultLatencyBuckets,
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

		OutboxOldestRowAgeSeconds: prometheus.NewGauge(
			prometheus.GaugeOpts{
				Namespace: "orderflow",
				Subsystem: "outbox",
				Name:      "oldest_row_age_seconds",
				Help:      "Age in seconds of the oldest pending record in the transactional outbox table.",
			},
		),

		KafkaConsumerLag: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: "orderflow",
				Subsystem: "kafka",
				Name:      "consumer_lag",
				Help:      "Current Kafka consumer lag per partition.",
			},
			[]string{"topic", "partition"},
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
				Buckets:   defaultLatencyBuckets,
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
			[]string{"event_type", "reason"},
		),

		SagaLatencySeconds: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Namespace: "orderflow",
				Subsystem: "saga",
				Name:      "duration_seconds",
				Help:      "End-to-end latency from reservation creation to terminal state.",
				Buckets:   defaultLatencyBuckets,
			},
			[]string{"outcome"},
		),
	}

	// Register Go runtime metrics (ignore already-registered error when using DefaultRegisterer)
	_ = registerer.Register(collectors.NewGoCollector())
	_ = registerer.Register(collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))

	// Register all application collectors with the registerer
	registerer.MustRegister(
		m.HTTPRequestsTotal,
		m.HTTPRequestDuration,
		m.RateLimitRejectedTotal,
		m.RedisCacheHitsTotal,
		m.RedisCacheMissesTotal,
		m.RedisFailOpenTotal,
		m.OutboxBacklogRecords,
		m.OutboxOldestRowAgeSeconds,
		m.KafkaConsumerLag,
		m.WorkerEventsProcessedTotal,
		m.WorkerEventDurationSeconds,
		m.WorkerRetriesTotal,
		m.WorkerDLQEventsTotal,
		m.SagaLatencySeconds,
	)

	return m
}

// RegisterPoolStats registers dynamic connection pool statistics for a pgx connection pool.
func (m *Metrics) RegisterPoolStats(pool *pgxpool.Pool, name string) {
	if m == nil || m.Registry == nil || pool == nil {
		return
	}
	collector := NewPgxPoolCollector(pool, name)
	_ = m.Registry.Register(collector)
}
