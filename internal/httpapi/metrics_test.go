package httpapi_test

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/theRiddler9/OrderFlow/internal/httpapi"
	"github.com/theRiddler9/OrderFlow/internal/metrics"
)

func TestMetricsEndpointAndInstrumentation(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := metrics.New(reg)

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	authService := newStubAuthenticator()
	apiServer := httpapi.NewServer(nil, nil, authService, nil, 2, 5, logger).WithMetrics(m)
	handler := apiServer.Handler()

	// 1. Send request to /healthz
	reqHealth := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	recHealth := httptest.NewRecorder()
	handler.ServeHTTP(recHealth, reqHealth)

	if recHealth.Code != http.StatusOK {
		t.Fatalf("expected 200 OK from /healthz, got %d", recHealth.Code)
	}

	// 2. Query /metrics
	reqMetrics := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	recMetrics := httptest.NewRecorder()
	handler.ServeHTTP(recMetrics, reqMetrics)

	if recMetrics.Code != http.StatusOK {
		t.Fatalf("expected 200 OK from /metrics, got %d", recMetrics.Code)
	}

	body := recMetrics.Body.String()
	if !strings.Contains(body, "orderflow_http_requests_total") {
		t.Fatalf("expected /metrics to contain 'orderflow_http_requests_total', got:\n%s", body)
	}
	if !strings.Contains(body, `path="GET /healthz"`) && !strings.Contains(body, `path="/healthz"`) {
		t.Fatalf("expected /metrics to contain healthz path label, got:\n%s", body)
	}
	if !strings.Contains(body, "orderflow_http_request_duration_seconds") {
		t.Fatalf("expected /metrics to contain request duration histogram, got:\n%s", body)
	}
}

func TestRateLimitMetricsIncrement(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := metrics.New(reg)

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	authService := newStubAuthenticator()

	// Rate limiter with limit 1
	stub := &stubLimiter{
		allowed:    false,
		remaining:  0,
		retryAfter: 30 * time.Second,
	}

	apiServer := httpapi.NewServer(nil, nil, authService, stub, 1, 1, logger).WithMetrics(m)
	handler := apiServer.Handler()

	req := httptest.NewRequest(http.MethodPost, "/v1/auth/login", strings.NewReader(`{"email":"test@example.com","password":"pass"}`))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429 Too Many Requests, got %d", rec.Code)
	}

	// Scrape metrics and check ratelimit_rejected_total
	reqMetrics := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	recMetrics := httptest.NewRecorder()
	handler.ServeHTTP(recMetrics, reqMetrics)

	body := recMetrics.Body.String()
	if !strings.Contains(body, "orderflow_ratelimit_rejected_total") {
		t.Fatalf("expected orderflow_ratelimit_rejected_total in metrics output, got:\n%s", body)
	}
}

func TestMetricsRoutePatternCardinality(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := metrics.New(reg)

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	authService := newStubAuthenticator()
	apiServer := httpapi.NewServer(nil, nil, authService, nil, 10, 10, logger).WithMetrics(m)
	handler := apiServer.Handler()

	rawUUID := "550e8400-e29b-41d4-a716-446655440000"

	// 1. Matched route with param
	req1 := httptest.NewRequest(http.MethodGet, "/v1/reservations/"+rawUUID, nil)
	rec1 := httptest.NewRecorder()
	handler.ServeHTTP(rec1, req1)

	// 2. Random 404 route
	req2 := httptest.NewRequest(http.MethodGet, "/arbitrary/unknown/path/99999", nil)
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req2)

	// Gather metrics from registry
	mfs, err := reg.Gather()
	if err != nil {
		t.Fatalf("gather metrics: %v", err)
	}

	for _, mf := range mfs {
		if mf.GetName() != "orderflow_http_requests_total" {
			continue
		}
		for _, metric := range mf.GetMetric() {
			for _, label := range metric.GetLabel() {
				if label.GetName() == "path" {
					val := label.GetValue()
					if strings.Contains(val, rawUUID) {
						t.Errorf("cardinality leak: metric contains raw UUID %q instead of route pattern", val)
					}
					if strings.Contains(val, "99999") {
						t.Errorf("cardinality leak: 404 metric contains raw path %q instead of 'unmatched'", val)
					}
				}
			}
		}
	}
}
