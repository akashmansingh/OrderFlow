package httpapi_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/theRiddler9/OrderFlow/internal/auth"
	"github.com/theRiddler9/OrderFlow/internal/cache"
	"github.com/theRiddler9/OrderFlow/internal/httpapi"
	"github.com/theRiddler9/OrderFlow/internal/reservation"
)

type stubLimiter struct {
	allowed    bool
	remaining  int
	retryAfter time.Duration
	err        error
}

func (s *stubLimiter) Allow(ctx context.Context, key string, limit int, window time.Duration) (bool, int, time.Duration, error) {
	return s.allowed, s.remaining, s.retryAfter, s.err
}

func TestLoginRateLimiting(t *testing.T) {
	server := miniredis.RunT(t)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	redisClient, err := cache.New(server.Addr(), "", time.Second, logger)
	if err != nil {
		t.Fatalf("cache.New() error = %v", err)
	}
	defer redisClient.Close()

	authenticator := newStubAuthenticator()
	apiServer := httpapi.NewServer(nil, nil, authenticator, redisClient, 2, 5, logger)
	handler := apiServer.Handler()

	body := `{"email":"test@example.com","password":"valid-password-123"}`

	// Request 1: allowed, remaining 1
	req1 := httptest.NewRequest(http.MethodPost, "/v1/auth/login", strings.NewReader(body))
	req1.RemoteAddr = "192.168.1.100:1234"
	rec1 := httptest.NewRecorder()
	handler.ServeHTTP(rec1, req1)

	if rec1.Code != http.StatusOK {
		t.Fatalf("req 1 status = %d, want 200", rec1.Code)
	}
	if rec1.Header().Get("X-RateLimit-Limit") != "2" {
		t.Fatalf("X-RateLimit-Limit = %q, want 2", rec1.Header().Get("X-RateLimit-Limit"))
	}
	if rec1.Header().Get("X-RateLimit-Remaining") != "1" {
		t.Fatalf("X-RateLimit-Remaining = %q, want 1", rec1.Header().Get("X-RateLimit-Remaining"))
	}

	// Request 2: allowed, remaining 0
	req2 := httptest.NewRequest(http.MethodPost, "/v1/auth/login", strings.NewReader(body))
	req2.RemoteAddr = "192.168.1.100:1234"
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req2)

	if rec2.Code != http.StatusOK {
		t.Fatalf("req 2 status = %d, want 200", rec2.Code)
	}
	if rec2.Header().Get("X-RateLimit-Remaining") != "0" {
		t.Fatalf("X-RateLimit-Remaining = %q, want 0", rec2.Header().Get("X-RateLimit-Remaining"))
	}

	// Request 3: rate limited (429)
	req3 := httptest.NewRequest(http.MethodPost, "/v1/auth/login", strings.NewReader(body))
	req3.RemoteAddr = "192.168.1.100:1234"
	rec3 := httptest.NewRecorder()
	handler.ServeHTTP(rec3, req3)

	if rec3.Code != http.StatusTooManyRequests {
		t.Fatalf("req 3 status = %d, want 429", rec3.Code)
	}
	if rec3.Header().Get("Retry-After") == "" {
		t.Fatal("expected Retry-After header on 429 response")
	}

	// Different IP: still allowed
	reqOther := httptest.NewRequest(http.MethodPost, "/v1/auth/login", strings.NewReader(body))
	reqOther.RemoteAddr = "192.168.1.101:1234"
	recOther := httptest.NewRecorder()
	handler.ServeHTTP(recOther, reqOther)

	if recOther.Code != http.StatusOK {
		t.Fatalf("req from other IP status = %d, want 200", recOther.Code)
	}
}

func TestReservationRateLimitingByUserSubject(t *testing.T) {
	server := miniredis.RunT(t)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	redisClient, err := cache.New(server.Addr(), "", time.Second, logger)
	if err != nil {
		t.Fatalf("cache.New() error = %v", err)
	}
	defer redisClient.Close()

	user1 := "00000000-0000-0000-0000-000000000001"
	user2 := "00000000-0000-0000-0000-000000000002"

	authenticator := newStubAuthenticator()
	authenticator.subjects["token-user-1"] = user1
	authenticator.subjects["token-user-2"] = user2

	reservations := newStubReservations()
	apiServer := httpapi.NewServer(nil, reservations, authenticator, redisClient, 5, 2, logger)
	handler := apiServer.Handler()

	body := `{"quantity":1}`
	eventID := "11111111-1111-1111-1111-111111111111"

	// User 1 - Request 1
	r1 := httptest.NewRequest(http.MethodPost, "/v1/events/"+eventID+"/reservations", strings.NewReader(body))
	r1.Header.Set("Authorization", "Bearer token-user-1")
	r1.Header.Set("Idempotency-Key", "idemp-1")
	w1 := httptest.NewRecorder()
	handler.ServeHTTP(w1, r1)
	if w1.Code != http.StatusCreated {
		t.Fatalf("user 1 req 1 status = %d, want 201", w1.Code)
	}

	// User 1 - Request 2
	r2 := httptest.NewRequest(http.MethodPost, "/v1/events/"+eventID+"/reservations", strings.NewReader(body))
	r2.Header.Set("Authorization", "Bearer token-user-1")
	r2.Header.Set("Idempotency-Key", "idemp-2")
	w2 := httptest.NewRecorder()
	handler.ServeHTTP(w2, r2)
	if w2.Code != http.StatusCreated {
		t.Fatalf("user 1 req 2 status = %d, want 201", w2.Code)
	}

	// User 1 - Request 3 (exceeds limit 2)
	r3 := httptest.NewRequest(http.MethodPost, "/v1/events/"+eventID+"/reservations", strings.NewReader(body))
	r3.Header.Set("Authorization", "Bearer token-user-1")
	r3.Header.Set("Idempotency-Key", "idemp-3")
	w3 := httptest.NewRecorder()
	handler.ServeHTTP(w3, r3)
	if w3.Code != http.StatusTooManyRequests {
		t.Fatalf("user 1 req 3 status = %d, want 429", w3.Code)
	}

	// User 2 - Request 1 should still succeed (separate user quota)
	rUser2 := httptest.NewRequest(http.MethodPost, "/v1/events/"+eventID+"/reservations", strings.NewReader(body))
	rUser2.Header.Set("Authorization", "Bearer token-user-2")
	rUser2.Header.Set("Idempotency-Key", "idemp-4")
	wUser2 := httptest.NewRecorder()
	handler.ServeHTTP(wUser2, rUser2)
	if wUser2.Code != http.StatusCreated {
		t.Fatalf("user 2 req 1 status = %d, want 201", wUser2.Code)
	}
}

func TestRateLimitFailsOpenOnLimiterError(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	limiter := &stubLimiter{err: errors.New("redis unavailable")}

	authenticator := newStubAuthenticator()
	apiServer := httpapi.NewServer(nil, nil, authenticator, limiter, 5, 5, logger)
	handler := apiServer.Handler()

	req := httptest.NewRequest(http.MethodPost, "/v1/auth/login", strings.NewReader(`{"email":"test@example.com","password":"valid-password-123"}`))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	// Should fail open and process the request normally
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK on limiter error fail-open, got %d", rec.Code)
	}
}

func TestConcurrentHTTPRateLimiting(t *testing.T) {
	server := miniredis.RunT(t)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	redisClient, err := cache.New(server.Addr(), "", time.Second, logger)
	if err != nil {
		t.Fatalf("cache.New() error = %v", err)
	}
	defer redisClient.Close()

	authenticator := newStubAuthenticator()
	limit := 10
	apiServer := httpapi.NewServer(nil, nil, authenticator, redisClient, limit, 20, logger)
	handler := apiServer.Handler()

	numRequests := 50
	var wg sync.WaitGroup
	startBarrier := make(chan struct{})
	var successCount atomic.Int64
	var rateLimitedCount atomic.Int64

	for i := 0; i < numRequests; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			<-startBarrier

			req := httptest.NewRequest(http.MethodPost, "/v1/auth/login", strings.NewReader(`{"email":"test@example.com","password":"valid-password-123"}`))
			req.RemoteAddr = "192.168.1.200:1234" // Same IP
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			switch rec.Code {
			case http.StatusOK:
				successCount.Add(1)
			case http.StatusTooManyRequests:
				rateLimitedCount.Add(1)
				if rec.Header().Get("Retry-After") == "" {
					t.Errorf("missing Retry-After header on 429 response")
				}
			default:
				t.Errorf("unexpected status code: %d", rec.Code)
			}
		}(i)
	}

	close(startBarrier)
	wg.Wait()

	if successCount.Load() != int64(limit) {
		t.Fatalf("successCount = %d, want %d", successCount.Load(), limit)
	}
	if rateLimitedCount.Load() != int64(numRequests-limit) {
		t.Fatalf("rateLimitedCount = %d, want %d", rateLimitedCount.Load(), numRequests-limit)
	}
}

// Helpers
func newStubAuthenticator() *stubAuth {
	return &stubAuth{
		subjects: make(map[string]string),
	}
}

type stubAuth struct {
	subjects map[string]string
}

func (s *stubAuth) Register(ctx context.Context, email, password string) (auth.User, error) {
	return auth.User{ID: "new-user", Email: email}, nil
}

func (s *stubAuth) Login(ctx context.Context, email, password string) (auth.TokenResponse, error) {
	return auth.TokenResponse{AccessToken: "sample-token", TokenType: "Bearer", ExpiresAt: time.Now().Add(time.Hour)}, nil
}

func (s *stubAuth) VerifyAccessToken(raw string) (string, error) {
	if sub, ok := s.subjects[raw]; ok {
		return sub, nil
	}
	return "sample-sub", nil
}

func newStubReservations() *stubRes {
	return &stubRes{}
}

type stubRes struct{}

func (s *stubRes) CreateEvent(ctx context.Context, in reservation.CreateEventInput) (reservation.Event, error) {
	return reservation.Event{ID: "event-1"}, nil
}

func (s *stubRes) GetEvent(ctx context.Context, id string) (reservation.Event, error) {
	return reservation.Event{ID: id}, nil
}

func (s *stubRes) ListEvents(ctx context.Context) ([]reservation.Event, error) {
	return []reservation.Event{}, nil
}

func (s *stubRes) CreateReservation(ctx context.Context, in reservation.CreateReservationInput) (reservation.Reservation, error) {
	return reservation.Reservation{ID: "res-1", EventID: in.EventID, CustomerID: in.CustomerID, Quantity: in.Quantity, Status: "held"}, nil
}

func (s *stubRes) GetReservationForCustomer(ctx context.Context, resID, custID string) (reservation.Reservation, error) {
	return reservation.Reservation{ID: resID, CustomerID: custID}, nil
}

func (s *stubRes) PayReservation(ctx context.Context, in reservation.PayReservationInput) (reservation.Reservation, error) {
	return reservation.Reservation{ID: in.ReservationID, CustomerID: in.CustomerID, Status: "held"}, nil
}

func TestRateLimitingDisabledByFlag(t *testing.T) {
	server := miniredis.RunT(t)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	redisClient, err := cache.New(server.Addr(), "", time.Second, logger)
	if err != nil {
		t.Fatalf("cache.New() error = %v", err)
	}
	defer redisClient.Close()

	authenticator := newStubAuthenticator()
	apiServer := httpapi.NewServer(nil, nil, authenticator, redisClient, 2, 5, logger).
		WithRateLimitEnabled(false)
	handler := apiServer.Handler()

	body := `{"email":"test@example.com","password":"valid-password-123"}`

	// Execute 3 requests; all must return 200 because rate limiting is disabled
	for i := 1; i <= 3; i++ {
		req := httptest.NewRequest(http.MethodPost, "/v1/auth/login", strings.NewReader(body))
		req.RemoteAddr = "192.168.1.100:1234"
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("req %d status = %d, want 200 when rate limiting is disabled", i, rec.Code)
		}
	}
}
