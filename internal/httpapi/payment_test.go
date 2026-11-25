package httpapi_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/theRiddler9/OrderFlow/internal/reservation"
)

func payHeaders(key string) map[string]string {
	return map[string]string{
		"Authorization":   "Bearer valid-token",
		"Idempotency-Key": key,
	}
}

func TestPayReservationUsesAuthenticatedSubjectAndReturnsAccepted(t *testing.T) {
	customerID := uuid.NewString()
	reservationID := uuid.NewString()
	key := uuid.NewString()
	reservations := &fakeReservationService{
		payResult: reservation.Reservation{
			ID:         reservationID,
			CustomerID: customerID,
			Status:     "held",
			Payment:    &reservation.Payment{Status: reservation.PaymentProcessing, Attempts: 1},
		},
	}

	// Mixed case and padding in the scenario are normalised before reaching the service.
	response := performRequest(t, newTestServer(reservations, &fakeAuthenticator{subject: customerID}).Handler(),
		http.MethodPost, "/v1/reservations/"+reservationID+"/payment",
		map[string]any{"scenario": " Decline "}, payHeaders(key))

	if response.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d; body = %s", response.Code, http.StatusAccepted, response.Body.String())
	}
	if len(reservations.paidInputs) != 1 {
		t.Fatalf("PayReservation calls = %d, want 1", len(reservations.paidInputs))
	}
	got := reservations.paidInputs[0]
	want := reservation.PayReservationInput{
		ReservationID:  reservationID,
		CustomerID:     customerID,
		Scenario:       reservation.PaymentScenarioDecline,
		IdempotencyKey: key,
	}
	if got != want {
		t.Fatalf("PayReservation input = %+v, want %+v", got, want)
	}

	var body struct {
		Status  string `json:"status"`
		Payment struct {
			Status string `json:"status"`
		} `json:"payment"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Status != "held" || body.Payment.Status != reservation.PaymentProcessing {
		t.Fatalf("response status=%q payment=%q, want held/processing", body.Status, body.Payment.Status)
	}
}

func TestPayReservationRequiresIdempotencyKey(t *testing.T) {
	customerID := uuid.NewString()
	reservations := &fakeReservationService{}

	response := performRequest(t, newTestServer(reservations, &fakeAuthenticator{subject: customerID}).Handler(),
		http.MethodPost, "/v1/reservations/"+uuid.NewString()+"/payment",
		map[string]any{"scenario": "approve"}, map[string]string{"Authorization": "Bearer valid-token"})

	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusBadRequest)
	}
	if len(reservations.paidInputs) != 0 {
		t.Fatal("service must not be called without an Idempotency-Key")
	}
}

func TestPayReservationRequiresBearerToken(t *testing.T) {
	reservations := &fakeReservationService{}

	response := performRequest(t, newTestServer(reservations, &fakeAuthenticator{}).Handler(),
		http.MethodPost, "/v1/reservations/"+uuid.NewString()+"/payment",
		map[string]any{"scenario": "approve"}, map[string]string{"Idempotency-Key": uuid.NewString()})

	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusUnauthorized)
	}
	if len(reservations.paidInputs) != 0 {
		t.Fatal("service must not be called without authentication")
	}
}

func TestPayReservationRejectsCardDetails(t *testing.T) {
	reservations := &fakeReservationService{}

	// The API never accepts card data: unknown fields are a validation error.
	response := performRequest(t, newTestServer(reservations, &fakeAuthenticator{subject: uuid.NewString()}).Handler(),
		http.MethodPost, "/v1/reservations/"+uuid.NewString()+"/payment",
		map[string]any{"scenario": "approve", "card_number": "4242424242424242"}, payHeaders(uuid.NewString()))

	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusBadRequest)
	}
	if len(reservations.paidInputs) != 0 {
		t.Fatal("service must not be called when the body contains card fields")
	}
}

func TestPayReservationMapsServiceErrors(t *testing.T) {
	tests := []struct {
		err  error
		want int
	}{
		{reservation.ErrReservationNotFound, http.StatusNotFound},
		{reservation.ErrReservationNotPayable, http.StatusConflict},
		{reservation.ErrHoldExpired, http.StatusConflict},
		{reservation.ErrPaymentInProgress, http.StatusConflict},
		{reservation.ErrPaymentsAutomatic, http.StatusConflict},
		{reservation.ErrIdempotencyConflict, http.StatusConflict},
		{reservation.ErrInvalidPaymentScenario, http.StatusBadRequest},
		// Errors are matched through wrapping, not by identity.
		{fmt.Errorf("lock: %w", reservation.ErrHoldExpired), http.StatusConflict},
	}

	for _, test := range tests {
		t.Run(test.err.Error(), func(t *testing.T) {
			reservations := &fakeReservationService{payErr: test.err}

			response := performRequest(t, newTestServer(reservations, &fakeAuthenticator{subject: uuid.NewString()}).Handler(),
				http.MethodPost, "/v1/reservations/"+uuid.NewString()+"/payment",
				map[string]any{"scenario": "approve"}, payHeaders(uuid.NewString()))

			if response.Code != test.want {
				t.Fatalf("status = %d, want %d; body = %s", response.Code, test.want, response.Body.String())
			}
			var body map[string]string
			if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil || body["error"] == "" {
				t.Fatalf("body = %s, want an error message", response.Body.String())
			}
		})
	}
}
