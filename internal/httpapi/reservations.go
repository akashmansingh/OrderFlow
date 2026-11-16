package httpapi

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
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
	CustomerID string `json:"customer_id"`
	Quantity   int    `json:"quantity"`
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

func (server *Server) createReservation(response http.ResponseWriter, request *http.Request) {
	idempotencyKey := request.Header.Get("Idempotency-Key")
	if idempotencyKey == "" {
		writeJSON(response, http.StatusBadRequest, map[string]string{"error": "Idempotency-Key header is required"})
		return
	}

	var body createReservationRequest
	if err := decodeJSON(response, request, &body); err != nil {
		return
	}

	eventID := request.PathValue("eventID")
	requestHash := sha256.Sum256([]byte(fmt.Sprintf("%s:%s:%d", eventID, body.CustomerID, body.Quantity)))
	reservationRecord, err := server.service.CreateReservation(request.Context(), reservation.CreateReservationInput{
		EventID:        eventID,
		CustomerID:     body.CustomerID,
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
	reservationRecord, err := server.service.GetReservation(request.Context(), request.PathValue("reservationID"))
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
	case errors.Is(err, reservation.ErrIdempotencyConflict):
		writeJSON(response, http.StatusConflict, map[string]string{"error": err.Error()})
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
	if decoder.More() {
		writeJSON(response, http.StatusBadRequest, map[string]string{"error": "request body must contain one JSON value"})
		return errors.New("multiple JSON values in request")
	}

	return nil
}
