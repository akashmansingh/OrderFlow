package e2e

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"testing"
	"time"
)

type event struct {
	ID       string    `json:"id"`
	Name     string    `json:"name"`
	StartsAt time.Time `json:"starts_at"`
	Capacity int       `json:"capacity"`
}

type reservation struct {
	ID         string     `json:"id"`
	EventID    string     `json:"event_id"`
	CustomerID string     `json:"customer_id"`
	Quantity   int        `json:"quantity"`
	Status     string     `json:"status"`
	ExpiresAt  *time.Time `json:"expires_at"`
}

func TestHealth(t *testing.T) {
	requireE2E(t)

	response := request(t, http.MethodGet, "/healthz", nil, nil)
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		t.Fatalf("health status = %d, want %d", response.StatusCode, http.StatusOK)
	}
}

func TestReservationWorkflowPreventsOversellAndSupportsIdempotency(t *testing.T) {
	requireE2E(t)

	createdEvent := createEvent(t, 2)
	customerID := randomUUID(t)
	idempotencyKey := randomUUID(t)

	first := createReservation(t, createdEvent.ID, customerID, 2, idempotencyKey)
	if first.Status != "held" {
		t.Fatalf("reservation status = %q, want held", first.Status)
	}
	if first.ExpiresAt == nil {
		t.Fatal("held reservation must include expires_at")
	}

	retry := createReservation(t, createdEvent.ID, customerID, 2, idempotencyKey)
	if retry.ID != first.ID {
		t.Fatalf("idempotent retry created %q, want existing reservation %q", retry.ID, first.ID)
	}

	body := map[string]any{
		"customer_id": randomUUID(t),
		"quantity":    1,
	}
	response := request(t, http.MethodPost, "/v1/events/"+createdEvent.ID+"/reservations", body, map[string]string{
		"Idempotency-Key": randomUUID(t),
	})
	defer response.Body.Close()

	if response.StatusCode != http.StatusConflict {
		payload, _ := io.ReadAll(response.Body)
		t.Fatalf("oversell status = %d, want %d; body = %s", response.StatusCode, http.StatusConflict, payload)
	}
}

func createEvent(t *testing.T, capacity int) event {
	t.Helper()

	body := map[string]any{
		"name":      "E2E " + randomUUID(t),
		"starts_at": time.Now().UTC().Add(24 * time.Hour),
		"capacity":  capacity,
	}
	response := request(t, http.MethodPost, "/v1/events", body, nil)
	defer response.Body.Close()

	if response.StatusCode != http.StatusCreated {
		payload, _ := io.ReadAll(response.Body)
		t.Fatalf("create event status = %d, want %d; body = %s", response.StatusCode, http.StatusCreated, payload)
	}

	var created event
	decodeJSON(t, response.Body, &created)
	if created.ID == "" {
		t.Fatal("created event has no ID")
	}
	return created
}

func createReservation(t *testing.T, eventID, customerID string, quantity int, idempotencyKey string) reservation {
	t.Helper()

	response := request(t, http.MethodPost, "/v1/events/"+eventID+"/reservations", map[string]any{
		"customer_id": customerID,
		"quantity":    quantity,
	}, map[string]string{
		"Idempotency-Key": idempotencyKey,
	})
	defer response.Body.Close()

	if response.StatusCode != http.StatusCreated {
		payload, _ := io.ReadAll(response.Body)
		t.Fatalf("create reservation status = %d, want %d; body = %s", response.StatusCode, http.StatusCreated, payload)
	}

	var created reservation
	decodeJSON(t, response.Body, &created)
	return created
}

func request(t *testing.T, method, path string, body any, headers map[string]string) *http.Response {
	t.Helper()

	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal request body: %v", err)
		}
		reader = bytes.NewReader(encoded)
	}

	request, err := http.NewRequestWithContext(t.Context(), method, apiURL()+path, reader)
	if err != nil {
		t.Fatalf("create request: %v", err)
	}
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	for key, value := range headers {
		request.Header.Set(key, value)
	}

	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("request %s %s: %v; start the stack with docker compose up -d --wait", method, path, err)
	}
	return response
}

func decodeJSON(t *testing.T, body io.Reader, destination any) {
	t.Helper()
	if err := json.NewDecoder(body).Decode(destination); err != nil {
		t.Fatalf("decode JSON response: %v", err)
	}
}

func apiURL() string {
	return os.Getenv("ORDERFLOW_E2E_BASE_URL")
}

func requireE2E(t *testing.T) {
	t.Helper()
	if apiURL() == "" {
		t.Skip("set ORDERFLOW_E2E_BASE_URL to run live-stack end-to-end tests")
	}
}

func randomUUID(t *testing.T) string {
	t.Helper()

	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		t.Fatalf("generate UUID: %v", err)
	}
	value[6] = (value[6] & 0x0f) | 0x40
	value[8] = (value[8] & 0x3f) | 0x80

	return fmt.Sprintf("%s-%s-%s-%s-%s",
		hex.EncodeToString(value[0:4]),
		hex.EncodeToString(value[4:6]),
		hex.EncodeToString(value[6:8]),
		hex.EncodeToString(value[8:10]),
		hex.EncodeToString(value[10:16]),
	)
}
