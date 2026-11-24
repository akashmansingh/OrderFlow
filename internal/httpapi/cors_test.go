package httpapi_test

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/theRiddler9/OrderFlow/internal/httpapi"
	"github.com/theRiddler9/OrderFlow/internal/reservation"
)

type corsStubService struct{}

func (s *corsStubService) CreateEvent(context.Context, reservation.CreateEventInput) (reservation.Event, error) {
	return reservation.Event{}, nil
}

func (s *corsStubService) GetEvent(context.Context, string) (reservation.Event, error) {
	return reservation.Event{}, nil
}

func (s *corsStubService) ListEvents(context.Context) ([]reservation.Event, error) {
	return []reservation.Event{
		{ID: "event-1", Name: "Coldplay Live", Capacity: 50, Available: 50},
	}, nil
}

func (s *corsStubService) CreateReservation(context.Context, reservation.CreateReservationInput) (reservation.Reservation, error) {
	return reservation.Reservation{}, nil
}

func (s *corsStubService) GetReservationForCustomer(context.Context, string, string) (reservation.Reservation, error) {
	return reservation.Reservation{}, nil
}

func (s *corsStubService) PayReservation(context.Context, string, string) (reservation.Reservation, error) {
	return reservation.Reservation{}, nil
}

func TestCORSPreflightAndHeaders(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	auth := newStubAuthenticator()

	server := httpapi.NewServer(nil, &corsStubService{}, auth, nil, 5, 10, logger)
	handler := server.Handler()

	// 1. Test OPTIONS preflight
	req := httptest.NewRequest(http.MethodOptions, "/v1/events", nil)
	req.Header.Set("Origin", "http://localhost:5173")
	req.Header.Set("Access-Control-Request-Method", "POST")
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusNoContent {
		t.Fatalf("OPTIONS status = %d, want %d", recorder.Code, http.StatusNoContent)
	}

	if origin := recorder.Header().Get("Access-Control-Allow-Origin"); origin != "http://localhost:5173" {
		t.Errorf("Access-Control-Allow-Origin = %q, want http://localhost:5173", origin)
	}

	expose := recorder.Header().Get("Access-Control-Expose-Headers")
	if expose == "" {
		t.Errorf("expected Access-Control-Expose-Headers to be set, got empty")
	}

	// 2. Test GET /v1/events with CORS
	getReq := httptest.NewRequest(http.MethodGet, "/v1/events", nil)
	getReq.Header.Set("Origin", "http://localhost:5173")
	getReq.Header.Set("Authorization", "Bearer valid-token")
	getRecorder := httptest.NewRecorder()

	handler.ServeHTTP(getRecorder, getReq)

	if getRecorder.Code != http.StatusOK {
		t.Fatalf("GET /v1/events status = %d, want %d; body = %s", getRecorder.Code, http.StatusOK, getRecorder.Body.String())
	}

	if origin := getRecorder.Header().Get("Access-Control-Allow-Origin"); origin != "http://localhost:5173" {
		t.Errorf("GET Access-Control-Allow-Origin = %q, want http://localhost:5173", origin)
	}
}
