package httpapi

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Server exposes the HTTP interface for the reservation API.
type Server struct {
	database *pgxpool.Pool
	logger   *slog.Logger
	handler  http.Handler
}

// NewServer creates the API routes and middleware.
func NewServer(database *pgxpool.Pool, logger *slog.Logger) *Server {
	server := &Server{
		database: database,
		logger:   logger,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", server.health)
	mux.HandleFunc("GET /readyz", server.ready)
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
