package httpapi

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/theRiddler9/OrderFlow/internal/auth"
	"github.com/theRiddler9/OrderFlow/internal/reservation"
)

// reservationService is the reservation behavior required by the HTTP layer.
type reservationService interface {
	CreateEvent(context.Context, reservation.CreateEventInput) (reservation.Event, error)
	GetEvent(context.Context, string) (reservation.Event, error)
	CreateReservation(context.Context, reservation.CreateReservationInput) (reservation.Reservation, error)
	GetReservationForCustomer(context.Context, string, string) (reservation.Reservation, error)
}

// authenticator is the auth behavior required by the HTTP layer.
type authenticator interface {
	Register(context.Context, string, string) (auth.User, error)
	Login(context.Context, string, string) (auth.TokenResponse, error)
	VerifyAccessToken(string) (string, error)
}

// Server exposes the HTTP interface for the reservation API.
type Server struct {
	database *pgxpool.Pool
	service  reservationService
	auth     authenticator
	logger   *slog.Logger
	handler  http.Handler
}

// NewServer creates the API routes and middleware.
func NewServer(database *pgxpool.Pool, service reservationService, authService authenticator, logger *slog.Logger) *Server {
	server := &Server{
		database: database,
		service:  service,
		auth:     authService,
		logger:   logger,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", server.health)
	mux.HandleFunc("GET /readyz", server.ready)
	mux.HandleFunc("POST /v1/auth/register", server.register)
	mux.HandleFunc("POST /v1/auth/login", server.login)
	mux.Handle("POST /v1/events", server.requireAuth(http.HandlerFunc(server.createEvent)))
	mux.Handle("GET /v1/events/{eventID}", server.requireAuth(http.HandlerFunc(server.getEvent)))
	mux.Handle("POST /v1/events/{eventID}/reservations", server.requireAuth(http.HandlerFunc(server.createReservation)))
	mux.Handle("GET /v1/reservations/{reservationID}", server.requireAuth(http.HandlerFunc(server.getReservation)))
	server.handler = server.recoverPanics(server.logRequests(mux))

	return server
}

// Handler returns the HTTP handler used by the process entry point and tests.
func (server *Server) Handler() http.Handler {
	return server.handler
}

func (server *Server) health(response http.ResponseWriter, _ *http.Request) {
	writeJSON(response, http.StatusOK, map[string]string{"status": "ok"})
}

func (server *Server) ready(response http.ResponseWriter, request *http.Request) {
	ctx, cancel := context.WithTimeout(request.Context(), time.Second)
	defer cancel()

	if err := server.database.Ping(ctx); err != nil {
		server.logger.Error("database readiness check failed", "error", err)
		writeJSON(response, http.StatusServiceUnavailable, map[string]string{"status": "unavailable"})
		return
	}

	writeJSON(response, http.StatusOK, map[string]string{"status": "ready"})
}

func (server *Server) logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		startedAt := time.Now()
		next.ServeHTTP(response, request)
		server.logger.Info("request completed",
			"method", request.Method,
			"path", request.URL.Path,
			"duration", time.Since(startedAt),
		)
	})
}

func (server *Server) recoverPanics(next http.Handler) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		defer func() {
			if recovered := recover(); recovered != nil {
				server.logger.Error("request panicked", "error", recovered)
				writeJSON(response, http.StatusInternalServerError, map[string]string{"error": "internal server error"})
			}
		}()

		next.ServeHTTP(response, request)
	})
}

func writeJSON(response http.ResponseWriter, status int, body any) {
	response.Header().Set("Content-Type", "application/json")
	response.WriteHeader(status)

	if err := json.NewEncoder(response).Encode(body); err != nil {
		return
	}
}
