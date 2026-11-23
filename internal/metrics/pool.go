package metrics

import (
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
)

// PgxPoolCollector dynamically exposes pgx connection pool metrics to Prometheus.
type PgxPoolCollector struct {
	pool          *pgxpool.Pool
	acquireCount  *prometheus.Desc
	acquiredConns *prometheus.Desc
	idleConns     *prometheus.Desc
	maxConns      *prometheus.Desc
	totalConns    *prometheus.Desc
}

// NewPgxPoolCollector constructs a collector for a specific pgxpool instance.
func NewPgxPoolCollector(pool *pgxpool.Pool, name string) *PgxPoolCollector {
	labels := prometheus.Labels{"pool": name}
	return &PgxPoolCollector{
		pool: pool,
		acquireCount: prometheus.NewDesc(
			"orderflow_pgxpool_acquire_count_total",
			"Cumulative count of successful connection acquires from the pool.",
			nil, labels,
		),
		acquiredConns: prometheus.NewDesc(
			"orderflow_pgxpool_acquired_connections",
			"Current number of active acquired connections.",
			nil, labels,
		),
		idleConns: prometheus.NewDesc(
			"orderflow_pgxpool_idle_connections",
			"Current number of idle connections in the pool.",
			nil, labels,
		),
		maxConns: prometheus.NewDesc(
			"orderflow_pgxpool_max_connections",
			"Maximum configured number of connections allowed in the pool.",
			nil, labels,
		),
		totalConns: prometheus.NewDesc(
			"orderflow_pgxpool_total_connections",
			"Current total number of connections (active + idle) in the pool.",
			nil, labels,
		),
	}
}

// Describe sends the super-set of all possible descriptors of metrics the Collector will collect.
func (c *PgxPoolCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.acquireCount
	ch <- c.acquiredConns
	ch <- c.idleConns
	ch <- c.maxConns
	ch <- c.totalConns
}

// Collect is called by the Prometheus registry when collecting metrics.
func (c *PgxPoolCollector) Collect(ch chan<- prometheus.Metric) {
	if c.pool == nil {
		return
	}
	stat := c.pool.Stat()
	ch <- prometheus.MustNewConstMetric(c.acquireCount, prometheus.CounterValue, float64(stat.AcquireCount()))
	ch <- prometheus.MustNewConstMetric(c.acquiredConns, prometheus.GaugeValue, float64(stat.AcquiredConns()))
	ch <- prometheus.MustNewConstMetric(c.idleConns, prometheus.GaugeValue, float64(stat.IdleConns()))
	ch <- prometheus.MustNewConstMetric(c.maxConns, prometheus.GaugeValue, float64(stat.MaxConns()))
	ch <- prometheus.MustNewConstMetric(c.totalConns, prometheus.GaugeValue, float64(stat.TotalConns()))
}
