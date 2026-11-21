package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/theRiddler9/OrderFlow/internal/auth"
)

type authenticatedSubjectKey struct{}

type registerRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

func (server *Server) register(response http.ResponseWriter, request *http.Request) {
	var body registerRequest
	if err := decodeJSON(response, request, &body); err != nil {
		return
	}

	user, err := server.auth.Register(request.Context(), body.Email, body.Password)
	if err != nil {
		server.writeAuthError(response, err)
		return
	}

	writeJSON(response, http.StatusCreated, user)
}

func (server *Server) login(response http.ResponseWriter, request *http.Request) {
	var body loginRequest
	if err := decodeJSON(response, request, &body); err != nil {
		return
	}

	token, err := server.auth.Login(request.Context(), body.Email, body.Password)
	if err != nil {
		server.writeAuthError(response, err)
		return
	}

	writeJSON(response, http.StatusOK, token)
}

func (server *Server) requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		scheme, rawToken, found := strings.Cut(strings.TrimSpace(request.Header.Get("Authorization")), " ")
		rawToken = strings.TrimSpace(rawToken)
		if !found || !strings.EqualFold(scheme, "Bearer") || rawToken == "" || strings.ContainsAny(rawToken, " \t\r\n") {
			server.writeUnauthorized(response)
			return
		}

		subject, err := server.auth.VerifyAccessToken(rawToken)
		if err != nil {
			server.writeUnauthorized(response)
			return
		}

		ctx := context.WithValue(request.Context(), authenticatedSubjectKey{}, subject)
		next.ServeHTTP(response, request.WithContext(ctx))
	})
}

func subjectFromContext(ctx context.Context) (string, bool) {
	subject, ok := ctx.Value(authenticatedSubjectKey{}).(string)
	return subject, ok && subject != ""
}

func (server *Server) writeAuthError(response http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, auth.ErrInvalidEmail), errors.Is(err, auth.ErrInvalidPassword):
		writeJSON(response, http.StatusBadRequest, map[string]string{"error": err.Error()})
	case errors.Is(err, auth.ErrDuplicateEmail):
		writeJSON(response, http.StatusConflict, map[string]string{"error": err.Error()})
	case errors.Is(err, auth.ErrInvalidCredentials):
		server.writeUnauthorized(response)
	default:
		server.logger.Error("auth request failed", "error", err)
		writeJSON(response, http.StatusBadRequest, map[string]string{"error": err.Error()})
	}
}

func (server *Server) writeUnauthorized(response http.ResponseWriter) {
	response.Header().Set("WWW-Authenticate", "Bearer")
	writeJSON(response, http.StatusUnauthorized, map[string]string{"error": "authentication required"})
}
