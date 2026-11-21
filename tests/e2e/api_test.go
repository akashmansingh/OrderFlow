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
	"strings"
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

type user struct {
	ID        string    `json:"id"`
	Email     string    `json:"email"`
	CreatedAt time.Time `json:"created_at"`
}

type tokenResponse struct {
	AccessToken string    `json:"access_token"`
	TokenType   string    `json:"token_type"`
	ExpiresAt   time.Time `json:"expires_at"`
}

func TestHealth(t *testing.T) {
	requireE2E(t)

	response := request(t, http.MethodGet, "/healthz", nil, nil)
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		t.Fatalf("health status = %d, want %d", response.StatusCode, http.StatusOK)
	}
}

func TestRegistrationAndLogin(t *testing.T) {
	requireE2E(t)

	email := "e2e-" + randomUUID(t) + "@example.com"
	password := "correct horse battery"
	created := register(t, email, password)
	if created.Email != email {
		t.Fatalf("registered email = %q, want %q", created.Email, email)
	}

	duplicate := request(t, http.MethodPost, "/v1/auth/register", map[string]any{
		"email":    strings.ToUpper(email),
		"password": password,
	}, nil)
	defer duplicate.Body.Close()
	if duplicate.StatusCode != http.StatusConflict {
		payload, _ := io.ReadAll(duplicate.Body)
		t.Fatalf("duplicate registration status = %d, want %d; body = %s", duplicate.StatusCode, http.StatusConflict, payload)
	}

	invalidLogin := request(t, http.MethodPost, "/v1/auth/login", map[string]any{
		"email":    email,
		"password": "wrong password value",
	}, nil)
	defer invalidLogin.Body.Close()
	if invalidLogin.StatusCode != http.StatusUnauthorized {
		payload, _ := io.ReadAll(invalidLogin.Body)
		t.Fatalf("invalid login status = %d, want %d; body = %s", invalidLogin.StatusCode, http.StatusUnauthorized, payload)
	}

	token := login(t, email, password)
	if token.TokenType != "Bearer" || token.AccessToken == "" {
		t.Fatalf("login token = %+v, want a non-empty Bearer token", token)
	}
	if !token.ExpiresAt.After(time.Now()) {
		t.Fatalf("token expiry = %s, want a future time", token.ExpiresAt)
	}
}

func TestReservationWorkflowPreventsOversellAndSupportsIdempotency(t *testing.T) {
	requireE2E(t)

	_, token := registerAndLogin(t)
	createdEvent := createEvent(t, token.AccessToken, 2)
	idempotencyKey := randomUUID(t)

	first := createReservation(t, token.AccessToken, createdEvent.ID, 2, idempotencyKey)
	if first.Status != "held" {
		t.Fatalf("reservation status = %q, want held", first.Status)
	}
	if first.ExpiresAt == nil {
		t.Fatal("held reservation must include expires_at")
	}

	retry := createReservation(t, token.AccessToken, createdEvent.ID, 2, idempotencyKey)
	if retry.ID != first.ID {
		t.Fatalf("idempotent retry created %q, want existing reservation %q", retry.ID, first.ID)
	}

	response := authenticatedRequest(t, token.AccessToken, http.MethodPost, "/v1/events/"+createdEvent.ID+"/reservations", map[string]any{
		"quantity": 1,
	}, map[string]string{
		"Idempotency-Key": randomUUID(t),
	})
	defer response.Body.Close()

	if response.StatusCode != http.StatusConflict {
		payload, _ := io.ReadAll(response.Body)
		t.Fatalf("oversell status = %d, want %d; body = %s", response.StatusCode, http.StatusConflict, payload)
	}
}

func TestReservationStatusIsOwnedByAuthenticatedUser(t *testing.T) {
	requireE2E(t)

	owner, ownerToken := registerAndLogin(t)
	_, otherToken := registerAndLogin(t)
	createdEvent := createEvent(t, ownerToken.AccessToken, 1)
	created := createReservation(t, ownerToken.AccessToken, createdEvent.ID, 1, randomUUID(t))
	if created.CustomerID != owner.ID {
		t.Fatalf("reservation customer = %q, want authenticated user %q", created.CustomerID, owner.ID)
	}

	ownerResponse := authenticatedRequest(t, ownerToken.AccessToken, http.MethodGet, "/v1/reservations/"+created.ID, nil, nil)
	defer ownerResponse.Body.Close()
	if ownerResponse.StatusCode != http.StatusOK {
		payload, _ := io.ReadAll(ownerResponse.Body)
		t.Fatalf("owner status = %d, want %d; body = %s", ownerResponse.StatusCode, http.StatusOK, payload)
	}
	var ownerResult reservation
	decodeJSON(t, ownerResponse.Body, &ownerResult)
	if ownerResult.ID != created.ID {
		t.Fatalf("owner read reservation %q, want %q", ownerResult.ID, created.ID)
	}

	otherResponse := authenticatedRequest(t, otherToken.AccessToken, http.MethodGet, "/v1/reservations/"+created.ID, nil, nil)
	defer otherResponse.Body.Close()
	if otherResponse.StatusCode != http.StatusNotFound {
		payload, _ := io.ReadAll(otherResponse.Body)
		t.Fatalf("other user status = %d, want %d; body = %s", otherResponse.StatusCode, http.StatusNotFound, payload)
	}

	anonymous := request(t, http.MethodGet, "/v1/reservations/"+created.ID, nil, nil)
	defer anonymous.Body.Close()
	if anonymous.StatusCode != http.StatusUnauthorized {
		payload, _ := io.ReadAll(anonymous.Body)
		t.Fatalf("anonymous status = %d, want %d; body = %s", anonymous.StatusCode, http.StatusUnauthorized, payload)
	}
}

func register(t *testing.T, email, password string) user {
	t.Helper()

	response := request(t, http.MethodPost, "/v1/auth/register", map[string]any{
		"email":    email,
		"password": password,
	}, nil)
	defer response.Body.Close()

	if response.StatusCode != http.StatusCreated {
		payload, _ := io.ReadAll(response.Body)
		t.Fatalf("register status = %d, want %d; body = %s", response.StatusCode, http.StatusCreated, payload)
	}

	var created user
	decodeJSON(t, response.Body, &created)
	if created.ID == "" {
		t.Fatal("registered user has no ID")
	}
	return created
}

func login(t *testing.T, email, password string) tokenResponse {
	t.Helper()

	response := request(t, http.MethodPost, "/v1/auth/login", map[string]any{
		"email":    email,
		"password": password,
	}, nil)
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		payload, _ := io.ReadAll(response.Body)
		t.Fatalf("login status = %d, want %d; body = %s", response.StatusCode, http.StatusOK, payload)
	}

	var token tokenResponse
	decodeJSON(t, response.Body, &token)
	return token
}

func registerAndLogin(t *testing.T) (user, tokenResponse) {
	t.Helper()
	email := "e2e-" + randomUUID(t) + "@example.com"
	password := "correct horse battery"
	return register(t, email, password), login(t, email, password)
}

func createEvent(t *testing.T, accessToken string, capacity int) event {
	t.Helper()

	body := map[string]any{
		"name":      "E2E " + randomUUID(t),
		"starts_at": time.Now().UTC().Add(24 * time.Hour),
		"capacity":  capacity,
	}
	response := authenticatedRequest(t, accessToken, http.MethodPost, "/v1/events", body, nil)
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

func createReservation(t *testing.T, accessToken, eventID string, quantity int, idempotencyKey string) reservation {
	t.Helper()

	response := authenticatedRequest(t, accessToken, http.MethodPost, "/v1/events/"+eventID+"/reservations", map[string]any{
		"quantity": quantity,
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

func authenticatedRequest(t *testing.T, accessToken, method, path string, body any, headers map[string]string) *http.Response {
	t.Helper()

	merged := make(map[string]string, len(headers)+1)
	for key, value := range headers {
		merged[key] = value
	}
	merged["Authorization"] = "Bearer " + accessToken
	return request(t, method, path, body, merged)
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
