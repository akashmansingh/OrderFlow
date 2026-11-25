package httpapi

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/theRiddler9/OrderFlow/internal/auth"
	"github.com/theRiddler9/OrderFlow/internal/metrics"
	"github.com/theRiddler9/OrderFlow/internal/reservation"
)

// reservationService is the reservation behavior required by the HTTP layer.
type reservationService interface {
	CreateEvent(context.Context, reservation.CreateEventInput) (reservation.Event, error)
	GetEvent(context.Context, string) (reservation.Event, error)
	ListEvents(context.Context) ([]reservation.Event, error)
	CreateReservation(context.Context, reservation.CreateReservationInput) (reservation.Reservation, error)
	GetReservationForCustomer(context.Context, string, string) (reservation.Reservation, error)
	PayReservation(context.Context, reservation.PayReservationInput) (reservation.Reservation, error)
}

// authenticator is the auth behavior required by the HTTP layer.
type authenticator interface {
	Register(context.Context, string, string) (auth.User, error)
	Login(context.Context, string, string) (auth.TokenResponse, error)
	VerifyAccessToken(string) (string, error)
}

// RateLimiter is the rate limiting behavior required by the HTTP layer.
type RateLimiter = rateLimiter
type Server struct {
	database         *pgxpool.Pool
	service          reservationService
	auth             authenticator
	limiter          rateLimiter
	loginLimit       int
	reservationLimit int
	rateLimitEnabled bool
	logger           *slog.Logger
	metrics          *metrics.Metrics
	handler          http.Handler
}

// NewServer creates the API routes and middleware.
func NewServer(
	database *pgxpool.Pool,
	service reservationService,
	authService authenticator,
	limiter rateLimiter,
	loginLimit int,
	reservationLimit int,
	logger *slog.Logger,
) *Server {
	server := &Server{
		database:         database,
		service:          service,
		auth:             authService,
		limiter:          limiter,
		loginLimit:       loginLimit,
		reservationLimit: reservationLimit,
		rateLimitEnabled: true,
		logger:           logger,
	}

	server.buildRoutes()
	return server
}

// WithRateLimitEnabled configures whether rate limiting middleware is enforced.
func (server *Server) WithRateLimitEnabled(enabled bool) *Server {
	server.rateLimitEnabled = enabled
	server.buildRoutes()
	return server
}

// WithMetrics attaches a Prometheus metrics collector to the server.
func (server *Server) WithMetrics(m *metrics.Metrics) *Server {
	server.metrics = m
	server.buildRoutes()
	return server
}

func (server *Server) buildRoutes() {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", server.health)
	mux.HandleFunc("GET /readyz", server.ready)
	mux.HandleFunc("GET /metrics", server.metricsHandler)
	mux.HandleFunc("POST /v1/auth/register", server.register)
	mux.Handle("POST /v1/auth/login", server.rateLimit(server.loginLimit, time.Minute, loginKey)(http.HandlerFunc(server.login)))
	mux.Handle("POST /v1/events", server.requireAuth(http.HandlerFunc(server.createEvent)))
	mux.Handle("GET /v1/events", server.requireAuth(http.HandlerFunc(server.listEvents)))
	mux.Handle("GET /v1/events/{eventID}", server.requireAuth(http.HandlerFunc(server.getEvent)))
	mux.Handle("POST /v1/events/{eventID}/reservations", server.requireAuth(server.rateLimit(server.reservationLimit, time.Minute, reservationKey)(http.HandlerFunc(server.createReservation))))
	mux.Handle("GET /v1/reservations/{reservationID}", server.requireAuth(http.HandlerFunc(server.getReservation)))
	mux.Handle("POST /v1/reservations/{reservationID}/payment", server.requireAuth(server.rateLimit(server.reservationLimit, time.Minute, paymentKey)(http.HandlerFunc(server.payReservation))))
	server.handler = server.recoverPanics(server.cors(TracingMiddleware(server.instrument(server.logRequests(mux)))))
}

// Handler returns the HTTP handler used by the process entry point and tests.
func (server *Server) Handler() http.Handler {
	return server.handler
}

func (server *Server) metricsHandler(response http.ResponseWriter, request *http.Request) {
	if server.metrics != nil && server.metrics.Registry != nil {
		if gatherer, ok := server.metrics.Registry.(prometheus.Gatherer); ok {
			promhttp.HandlerFor(gatherer, promhttp.HandlerOpts{}).ServeHTTP(response, request)
			return
		}
	}
	promhttp.Handler().ServeHTTP(response, request)
}

type statusRecorder struct {
	http.ResponseWriter
	statusCode int
}

func (rec *statusRecorder) WriteHeader(code int) {
	rec.statusCode = code
	rec.ResponseWriter.WriteHeader(code)
}

func (server *Server) instrument(next http.Handler) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if server.metrics == nil || request.URL.Path == "/metrics" {
			next.ServeHTTP(response, request)
			return
		}

		start := time.Now()
		rec := &statusRecorder{ResponseWriter: response, statusCode: http.StatusOK}
		next.ServeHTTP(rec, request)
		duration := time.Since(start).Seconds()

		pattern := request.Pattern
		if pattern == "" {
			pattern = "unmatched"
		} else {
			if parts := strings.SplitN(pattern, " ", 2); len(parts) == 2 {
				pattern = parts[1]
			}
		}

		server.metrics.HTTPRequestsTotal.WithLabelValues(request.Method, pattern, strconv.Itoa(rec.statusCode)).Inc()
		server.metrics.HTTPRequestDuration.WithLabelValues(request.Method, pattern).Observe(duration)
	})
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
