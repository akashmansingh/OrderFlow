package metrics_test

import (
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/theRiddler9/OrderFlow/internal/metrics"
)

var forbiddenLabelNames = []string{
	"id",
	"user_id",
	"userid",
	"customer_id",
	"customerid",
	"reservation_id",
	"reservationid",
	"event_id",
	"eventid",
	"order_id",
	"ip",
	"client_ip",
	"remote_addr",
	"token",
	"email",
}

func TestMetricLabelCardinalityAudit(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := metrics.New(reg)

	// Exercise every metric vector with sample bounded values to register series
	m.HTTPRequestsTotal.WithLabelValues("POST", "/v1/events/{id}/reservations", "201").Inc()
	m.HTTPRequestDuration.WithLabelValues("POST", "/v1/events/{id}/reservations").Observe(0.05)
	m.RateLimitRejectedTotal.WithLabelValues("reservation").Inc()
	m.RedisCacheHitsTotal.WithLabelValues("sold_out").Inc()
	m.RedisCacheMissesTotal.WithLabelValues("sold_out").Inc()
	m.RedisFailOpenTotal.WithLabelValues("rate_limit").Inc()
	m.OutboxBacklogRecords.Set(5)
	m.OutboxOldestRowAgeSeconds.Set(2.5)
	m.KafkaConsumerLag.WithLabelValues("reservation-events", "0").Set(0)
	m.WorkerEventsProcessedTotal.WithLabelValues("reservation.held.v1", "confirmed").Inc()
	m.WorkerEventDurationSeconds.WithLabelValues("reservation.held.v1").Observe(0.01)
	m.WorkerRetriesTotal.WithLabelValues("reservation.held.v1").Inc()
	m.WorkerDLQEventsTotal.WithLabelValues("reservation.held.v1", "poison_payload").Inc()
	m.SagaLatencySeconds.WithLabelValues("confirmed").Observe(0.12)

	mfs, err := reg.Gather()
	if err != nil {
		t.Fatalf("failed to gather registered metrics: %v", err)
	}

	if len(mfs) == 0 {
		t.Fatal("expected gathered metric families, got empty")
	}

	for _, mf := range mfs {
		name := mf.GetName()
		// Only audit application metrics in namespace "orderflow"
		if !strings.HasPrefix(name, "orderflow") {
			continue
		}

		for _, metric := range mf.GetMetric() {
			for _, label := range metric.GetLabel() {
				labelName := strings.ToLower(label.GetName())
				labelValue := label.GetValue()

				// Check forbidden label names
				for _, forbidden := range forbiddenLabelNames {
					if labelName == forbidden {
						t.Errorf("metric %q contains forbidden high-cardinality label name %q", name, labelName)
					}
				}

				// Check that path label never contains a raw UUID or numeric ID
				if labelName == "path" {
					if strings.Contains(labelValue, "12345") || len(labelValue) > 60 {
						t.Errorf("metric %q path label %q appears to contain unparameterized ID", name, labelValue)
					}
				}
			}
		}
	}
}
