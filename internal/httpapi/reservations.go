package httpapi

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/theRiddler9/OrderFlow/internal/reservation"
)

const maxRequestBodyBytes = 1 << 20

type createEventRequest struct {
	Name     string    `json:"name"`
	StartsAt time.Time `json:"starts_at"`
	Capacity int       `json:"capacity"`
}

type createReservationRequest struct {
	Quantity int `json:"quantity"`
}

func (server *Server) createEvent(response http.ResponseWriter, request *http.Request) {
	var body createEventRequest
	if err := decodeJSON(response, request, &body); err != nil {
		return
	}

	event, err := server.service.CreateEvent(request.Context(), reservation.CreateEventInput{
		Name:     strings.TrimSpace(body.Name),
		StartsAt: body.StartsAt,
		Capacity: body.Capacity,
	})
	if err != nil {
		server.writeServiceError(response, err)
		return
	}

	writeJSON(response, http.StatusCreated, event)
}

func (server *Server) getEvent(response http.ResponseWriter, request *http.Request) {
	event, err := server.service.GetEvent(request.Context(), request.PathValue("eventID"))
	if err != nil {
		server.writeServiceError(response, err)
		return
	}

	writeJSON(response, http.StatusOK, event)
}

func (server *Server) listEvents(response http.ResponseWriter, request *http.Request) {
	events, err := server.service.ListEvents(request.Context())
	if err != nil {
		server.writeServiceError(response, err)
		return
	}
	if events == nil {
		events = make([]reservation.Event, 0)
	}

	writeJSON(response, http.StatusOK, events)
}

func (server *Server) createReservation(response http.ResponseWriter, request *http.Request) {
	idempotencyKey := request.Header.Get("Idempotency-Key")
	if idempotencyKey == "" {
		writeJSON(response, http.StatusBadRequest, map[string]string{"error": "Idempotency-Key header is required"})
		return
	}

	customerID, ok := subjectFromContext(request.Context())
	if !ok {
		server.writeUnauthorized(response)
		return
	}

	var body createReservationRequest
	if err := decodeJSON(response, request, &body); err != nil {
		return
	}

	eventID := request.PathValue("eventID")
	requestHash := sha256.Sum256([]byte(fmt.Sprintf("%s:%s:%d", eventID, customerID, body.Quantity)))
	reservationRecord, err := server.service.CreateReservation(request.Context(), reservation.CreateReservationInput{
		EventID:        eventID,
		CustomerID:     customerID,
		Quantity:       body.Quantity,
		IdempotencyKey: idempotencyKey,
		RequestHash:    requestHash[:],
	})
	if err != nil {
		server.writeServiceError(response, err)
		return
	}

	writeJSON(response, http.StatusCreated, reservationRecord)
}

func (server *Server) getReservation(response http.ResponseWriter, request *http.Request) {
	customerID, ok := subjectFromContext(request.Context())
	if !ok {
		server.writeUnauthorized(response)
		return
	}

	reservationRecord, err := server.service.GetReservationForCustomer(request.Context(), request.PathValue("reservationID"), customerID)
	if err != nil {
		server.writeServiceError(response, err)
		return
	}

	writeJSON(response, http.StatusOK, reservationRecord)
}

func (server *Server) payReservation(response http.ResponseWriter, request *http.Request) {
	customerID, ok := subjectFromContext(request.Context())
	if !ok {
		server.writeUnauthorized(response)
		return
	}

	reservationID := request.PathValue("reservationID")
	reservationRecord, err := server.service.PayReservation(request.Context(), reservationID, customerID)
	if err != nil {
		server.writeServiceError(response, err)
		return
	}

	writeJSON(response, http.StatusOK, reservationRecord)
}

func (server *Server) writeServiceError(response http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, reservation.ErrEventNotFound), errors.Is(err, reservation.ErrReservationNotFound):
		writeJSON(response, http.StatusNotFound, map[string]string{"error": err.Error()})
	case errors.Is(err, reservation.ErrInsufficientInventory):
		writeJSON(response, http.StatusConflict, map[string]string{"error": err.Error()})
	case errors.Is(err, reservation.ErrIdempotencyConflict), errors.Is(err, reservation.ErrReservationNotHeld):
		writeJSON(response, http.StatusConflict, map[string]string{"error": err.Error()})
	case errors.Is(err, reservation.ErrHoldExpired):
		writeJSON(response, http.StatusGone, map[string]string{"error": err.Error()})
	default:
		server.logger.Error("request failed", "error", err)
		writeJSON(response, http.StatusBadRequest, map[string]string{"error": err.Error()})
	}
}

func decodeJSON(response http.ResponseWriter, request *http.Request, destination any) error {
	request.Body = http.MaxBytesReader(response, request.Body, maxRequestBodyBytes)
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(destination); err != nil {
		writeJSON(response, http.StatusBadRequest, map[string]string{"error": "request body must be valid JSON"})
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		writeJSON(response, http.StatusBadRequest, map[string]string{"error": "request body must contain one JSON value"})
		return errors.New("multiple JSON values in request")
	}

	return nil
}
