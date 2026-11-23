package httpapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/theRiddler9/OrderFlow/internal/auth"
	"github.com/theRiddler9/OrderFlow/internal/httpapi"
	"github.com/theRiddler9/OrderFlow/internal/reservation"
)

type fakeAuthenticator struct {
	subject           string
	verifyErr         error
	registerEmail     string
	registerPassword  string
	registerResult    auth.User
	registerErr       error
	loginEmail        string
	loginPassword     string
	loginResult       auth.TokenResponse
	loginErr          error
	verifiedRawTokens []string
}

func (authenticator *fakeAuthenticator) Register(_ context.Context, email, password string) (auth.User, error) {
	authenticator.registerEmail = email
	authenticator.registerPassword = password
	return authenticator.registerResult, authenticator.registerErr
}

func (authenticator *fakeAuthenticator) Login(_ context.Context, email, password string) (auth.TokenResponse, error) {
	authenticator.loginEmail = email
	authenticator.loginPassword = password
	return authenticator.loginResult, authenticator.loginErr
}

func (authenticator *fakeAuthenticator) VerifyAccessToken(rawToken string) (string, error) {
	authenticator.verifiedRawTokens = append(authenticator.verifiedRawTokens, rawToken)
	return authenticator.subject, authenticator.verifyErr
}

type fakeReservationService struct {
	createdInputs      []reservation.CreateReservationInput
	checkedCustomers   []string
	createdReservation reservation.Reservation
}

func (service *fakeReservationService) CreateEvent(context.Context, reservation.CreateEventInput) (reservation.Event, error) {
	return reservation.Event{}, nil
}

func (service *fakeReservationService) GetEvent(context.Context, string) (reservation.Event, error) {
	return reservation.Event{}, nil
}

func (service *fakeReservationService) ListEvents(context.Context) ([]reservation.Event, error) {
	return nil, nil
}

func (service *fakeReservationService) CreateReservation(_ context.Context, input reservation.CreateReservationInput) (reservation.Reservation, error) {
	service.createdInputs = append(service.createdInputs, input)
	if service.createdReservation.ID != "" {
		return service.createdReservation, nil
	}
	return reservation.Reservation{
		ID:         uuid.NewString(),
		EventID:    input.EventID,
		CustomerID: input.CustomerID,
		Quantity:   input.Quantity,
		Status:     "held",
	}, nil
}

func (service *fakeReservationService) GetReservationForCustomer(_ context.Context, _, customerID string) (reservation.Reservation, error) {
	service.checkedCustomers = append(service.checkedCustomers, customerID)
	return service.createdReservation, nil
}

func newTestServer(reservations *fakeReservationService, authenticator *fakeAuthenticator) *httpapi.Server {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return httpapi.NewServer(nil, reservations, authenticator, nil, 0, 0, logger)
}

func performRequest(t *testing.T, handler http.Handler, method, path string, body any, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()

	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal request body: %v", err)
		}
		reader = bytes.NewReader(encoded)
	}

	request := httptest.NewRequest(method, path, reader)
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	for key, value := range headers {
		request.Header.Set(key, value)
	}

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder
}

func TestPublicHealthRouteDoesNotRequireAuthentication(t *testing.T) {
	authenticator := &fakeAuthenticator{}
	response := performRequest(t, newTestServer(&fakeReservationService{}, authenticator).Handler(), http.MethodGet, "/healthz", nil, nil)

	if response.Code != http.StatusOK {
		t.Fatalf("health status = %d, want %d", response.Code, http.StatusOK)
	}
	if len(authenticator.verifiedRawTokens) != 0 {
		t.Fatal("health route must not verify a token")
	}
}

func TestProtectedRoutesRequireBearerToken(t *testing.T) {
	tests := map[string]map[string]string{
		"missing header": nil,
		"wrong scheme":   {"Authorization": "Basic abc123"},
		"empty token":    {"Authorization": "Bearer "},
		"extra token":    {"Authorization": "Bearer one two"},
	}

	for name, headers := range tests {
		t.Run(name, func(t *testing.T) {
			response := performRequest(t, newTestServer(&fakeReservationService{}, &fakeAuthenticator{}).Handler(), http.MethodGet, "/v1/events/"+uuid.NewString(), nil, headers)

			if response.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want %d", response.Code, http.StatusUnauthorized)
			}
			if got := response.Header().Get("WWW-Authenticate"); got != "Bearer" {
				t.Fatalf("WWW-Authenticate = %q, want Bearer", got)
			}
		})
	}

	authenticator := &fakeAuthenticator{verifyErr: auth.ErrInvalidToken}
	response := performRequest(t, newTestServer(&fakeReservationService{}, authenticator).Handler(), http.MethodGet, "/v1/events/"+uuid.NewString(), nil, map[string]string{
		"Authorization": "Bearer invalid-token",
	})
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("invalid token status = %d, want %d", response.Code, http.StatusUnauthorized)
	}
}

func TestCreateReservationUsesAuthenticatedSubject(t *testing.T) {
	customerID := uuid.NewString()
	eventID := uuid.NewString()
	authenticator := &fakeAuthenticator{subject: customerID}
	reservations := &fakeReservationService{}

	response := performRequest(t, newTestServer(reservations, authenticator).Handler(), http.MethodPost, "/v1/events/"+eventID+"/reservations", map[string]any{
		"quantity": 2,
	}, map[string]string{
		"Authorization":   "Bearer valid-token",
		"Idempotency-Key": uuid.NewString(),
	})

	if response.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body = %s", response.Code, http.StatusCreated, response.Body.String())
	}
	if len(reservations.createdInputs) != 1 {
		t.Fatalf("CreateReservation calls = %d, want 1", len(reservations.createdInputs))
	}
	created := reservations.createdInputs[0]
	if created.CustomerID != customerID || created.EventID != eventID || created.Quantity != 2 {
		t.Fatalf("CreateReservation input = %+v", created)
	}
	if len(created.RequestHash) == 0 {
		t.Fatal("CreateReservation input has no request hash")
	}
}

func TestCreateReservationRejectsClientSuppliedCustomerID(t *testing.T) {
	customerID := uuid.NewString()
	reservations := &fakeReservationService{}
	response := performRequest(t, newTestServer(reservations, &fakeAuthenticator{subject: customerID}).Handler(), http.MethodPost, "/v1/events/"+uuid.NewString()+"/reservations", map[string]any{
		"customer_id": uuid.NewString(),
		"quantity":    1,
	}, map[string]string{
		"Authorization":   "Bearer valid-token",
		"Idempotency-Key": uuid.NewString(),
	})

	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusBadRequest)
	}
	if len(reservations.createdInputs) != 0 {
		t.Fatal("reservation service must not be called with an invalid body")
	}
}

func TestGetReservationChecksAuthenticatedSubject(t *testing.T) {
	customerID := uuid.NewString()
	reservationID := uuid.NewString()
	reservations := &fakeReservationService{
		createdReservation: reservation.Reservation{
			ID:         reservationID,
			EventID:    uuid.NewString(),
			CustomerID: customerID,
			Quantity:   1,
			Status:     "held",
			ExpiresAt:  func() *time.Time { value := time.Now().Add(time.Minute); return &value }(),
		},
	}

	response := performRequest(t, newTestServer(reservations, &fakeAuthenticator{subject: customerID}).Handler(), http.MethodGet, "/v1/reservations/"+reservationID, nil, map[string]string{
		"Authorization": "Bearer valid-token",
	})

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", response.Code, http.StatusOK, response.Body.String())
	}
	if len(reservations.checkedCustomers) != 1 || reservations.checkedCustomers[0] != customerID {
		t.Fatalf("checked customers = %v, want [%s]", reservations.checkedCustomers, customerID)
	}
}

func TestRegisterAndLoginRoutes(t *testing.T) {
	user := auth.User{ID: uuid.NewString(), Email: "user@example.com", CreatedAt: time.Now().UTC()}
	token := auth.TokenResponse{AccessToken: "signed-token", TokenType: "Bearer", ExpiresAt: time.Now().Add(15 * time.Minute)}
	authenticator := &fakeAuthenticator{registerResult: user, loginResult: token}
	handler := newTestServer(&fakeReservationService{}, authenticator).Handler()

	registerResponse := performRequest(t, handler, http.MethodPost, "/v1/auth/register", map[string]any{
		"email":    "user@example.com",
		"password": "correct horse battery",
	}, nil)
	if registerResponse.Code != http.StatusCreated {
		t.Fatalf("register status = %d, want %d", registerResponse.Code, http.StatusCreated)
	}
	if authenticator.registerEmail != user.Email || authenticator.registerPassword != "correct horse battery" {
		t.Fatalf("register credentials = %q/%q", authenticator.registerEmail, authenticator.registerPassword)
	}

	loginResponse := performRequest(t, handler, http.MethodPost, "/v1/auth/login", map[string]any{
		"email":    "user@example.com",
		"password": "correct horse battery",
	}, nil)
	if loginResponse.Code != http.StatusOK {
		t.Fatalf("login status = %d, want %d", loginResponse.Code, http.StatusOK)
	}
	if authenticator.loginEmail != user.Email || authenticator.loginPassword != "correct horse battery" {
		t.Fatalf("login credentials = %q/%q", authenticator.loginEmail, authenticator.loginPassword)
	}
}

func TestAuthErrorResponses(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantStatus int
	}{
		{"invalid email", auth.ErrInvalidEmail, http.StatusBadRequest},
		{"duplicate email", auth.ErrDuplicateEmail, http.StatusConflict},
		{"invalid credentials", auth.ErrInvalidCredentials, http.StatusUnauthorized},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			authenticator := &fakeAuthenticator{loginErr: test.err}
			response := performRequest(t, newTestServer(&fakeReservationService{}, authenticator).Handler(), http.MethodPost, "/v1/auth/login", map[string]any{
				"email":    "user@example.com",
				"password": "correct horse battery",
			}, nil)

			if response.Code != test.wantStatus {
				t.Fatalf("status = %d, want %d", response.Code, test.wantStatus)
			}
			if !errors.Is(test.err, auth.ErrInvalidCredentials) && response.Header().Get("WWW-Authenticate") != "" {
				t.Fatalf("unexpected WWW-Authenticate header for %s", test.name)
			}
		})
	}
}
