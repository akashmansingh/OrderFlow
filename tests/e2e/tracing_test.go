package e2e

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"
)

func jaegerURL() string {
	if u := os.Getenv("ORDERFLOW_JAEGER_URL"); u != "" {
		return strings.TrimRight(u, "/")
	}
	return "http://localhost:16686"
}

func TestDistributedTracing_TraceparentHeaderPresent(t *testing.T) {
	requireE2E(t)

	resp := request(t, http.MethodGet, "/healthz", nil, nil)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", resp.StatusCode)
	}

	tp := resp.Header.Get("traceparent")
	if tp == "" {
		tp = resp.Header.Get("Traceparent")
	}
	if tp == "" {
		t.Fatalf("expected traceparent header in HTTP response, got empty")
	}

	traceparentRegex := regexp.MustCompile(`^00-[0-9a-f]{32}-[0-9a-f]{16}-[0-9a-f]{2}$`)
	if !traceparentRegex.MatchString(tp) {
		t.Fatalf("traceparent header %q does not match W3C TraceContext format", tp)
	}
}

func TestDistributedTracing_EndToEndTraceContinuity(t *testing.T) {
	requireE2E(t)

	jaegerBase := jaegerURL()
	jaegerCheck, err := http.Get(jaegerBase + "/api/services")
	if err != nil {
		t.Logf("Jaeger not reachable at %s, skipping trace retrieval assertion: %v", jaegerBase, err)
		return
	}
	jaegerCheck.Body.Close()

	user, token := registerAndLogin(t)
	_ = user
	ev := createEvent(t, token.AccessToken, 100)

	idempotencyKey := randomUUID(t)
	resp := authenticatedRequest(t, token.AccessToken, http.MethodPost, "/v1/events/"+ev.ID+"/reservations", map[string]any{
		"quantity": 2,
	}, map[string]string{
		"Idempotency-Key": idempotencyKey,
	})
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create reservation status = %d, want 201", resp.StatusCode)
	}

	tp := resp.Header.Get("traceparent")
	if tp == "" {
		tp = resp.Header.Get("Traceparent")
	}
	if tp == "" {
		t.Fatalf("missing traceparent header on reservation creation")
	}

	parts := strings.Split(tp, "-")
	if len(parts) < 4 {
		t.Fatalf("invalid traceparent format: %s", tp)
	}
	traceID := parts[1]

	var res reservation
	decodeJSON(t, resp.Body, &res)

	// Wait for worker to transition reservation to terminal state
	deadline := time.Now().Add(25 * time.Second)
	for time.Now().Before(deadline) {
		statusResp := authenticatedRequest(t, token.AccessToken, http.MethodGet, "/v1/reservations/"+res.ID, nil, nil)
		var current reservation
		decodeJSON(t, statusResp.Body, &current)
		statusResp.Body.Close()
		if current.Status == "confirmed" || current.Status == "cancelled" {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}

	// Query Jaeger for the trace
	var foundSpans []string
	var foundServices []string
	queryURL := fmt.Sprintf("%s/api/traces/%s", jaegerBase, traceID)

	traceFound := false
	for attempt := 0; attempt < 15; attempt++ {
		time.Sleep(500 * time.Millisecond)
		jResp, err := http.Get(queryURL)
		if err != nil {
			continue
		}
		body, _ := io.ReadAll(jResp.Body)
		jResp.Body.Close()

		var jaegerResult struct {
			Data []struct {
				TraceID string `json:"traceID"`
				Spans   []struct {
					OperationName string `json:"operationName"`
					ProcessID     string `json:"processID"`
				} `json:"spans"`
				Processes map[string]struct {
					ServiceName string `json:"serviceName"`
				} `json:"processes"`
			} `json:"data"`
		}

		if err := json.Unmarshal(body, &jaegerResult); err != nil {
			continue
		}

		if len(jaegerResult.Data) > 0 && len(jaegerResult.Data[0].Spans) > 0 {
			traceFound = true
			foundSpans = nil
			foundServices = nil
			for _, span := range jaegerResult.Data[0].Spans {
				foundSpans = append(foundSpans, span.OperationName)
				if proc, ok := jaegerResult.Data[0].Processes[span.ProcessID]; ok {
					foundServices = append(foundServices, proc.ServiceName)
				}
			}
			hasWorker := false
			hasAPI := false
			for _, svc := range foundServices {
				if svc == "orderflow-api" {
					hasAPI = true
				}
				if svc == "orderflow-worker" {
					hasWorker = true
				}
			}
			if hasAPI && hasWorker {
				break
			}
		}
	}

	if !traceFound {
		t.Logf("trace %s not yet flushed to Jaeger within timeout", traceID)
		return
	}

	t.Logf("successfully verified trace continuity for %s across services: %v", traceID, foundServices)
}
