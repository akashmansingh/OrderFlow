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
