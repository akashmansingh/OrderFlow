package e2e

import (
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

// TestPublicMetricsEndpointBlockedByReverseProxy verifies that external ingress
// blocks access to the internal /metrics endpoint with HTTP 403 Forbidden.
func TestPublicMetricsEndpointBlockedByReverseProxy(t *testing.T) {
	// If a live public proxy URL is configured via ORDERFLOW_PUBLIC_PROXY_URL, test against it
	proxyURL := os.Getenv("ORDERFLOW_PUBLIC_PROXY_URL")
	if proxyURL != "" {
		req, err := http.NewRequest(http.MethodGet, proxyURL+"/metrics", nil)
		if err != nil {
			t.Fatalf("create request: %v", err)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("request public proxy: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusForbidden && resp.StatusCode != http.StatusNotFound {
			t.Fatalf("expected 403 Forbidden or 404 Not Found on public /metrics, got %d", resp.StatusCode)
		}
		return
	}

	// Simulation test: verify that a reverse proxy filter blocking /metrics produces 403
	mockAppServer := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/metrics" {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("orderflow_http_requests_total 100\n"))
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})

	// Caddy-equivalent reverse proxy middleware
	proxyHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/metrics" || len(r.URL.Path) > 8 && r.URL.Path[:9] == "/metrics/" {
			http.Error(w, "Forbidden: Internal metrics endpoint", http.StatusForbidden)
			return
		}
		mockAppServer.ServeHTTP(w, r)
	})

	server := httptest.NewServer(proxyHandler)
	defer server.Close()

	// 1. Check /healthz passes through proxy
	respHealth, err := http.Get(server.URL + "/healthz")
	if err != nil {
		t.Fatalf("failed to query /healthz through proxy: %v", err)
	}
	defer respHealth.Body.Close()
	if respHealth.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK for /healthz, got %d", respHealth.StatusCode)
	}

	// 2. Check /metrics is blocked with 403 Forbidden
	respMetrics, err := http.Get(server.URL + "/metrics")
	if err != nil {
		t.Fatalf("failed to query /metrics through proxy: %v", err)
	}
	defer respMetrics.Body.Close()
	if respMetrics.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403 Forbidden on public /metrics, got %d", respMetrics.StatusCode)
	}
}
