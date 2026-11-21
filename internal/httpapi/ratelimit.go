package httpapi

import (
	"context"
	"fmt"
	"math"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type rateLimiter interface {
	Allow(ctx context.Context, key string, limit int, window time.Duration) (bool, int, time.Duration, error)
}

func (server *Server) rateLimit(limit int, window time.Duration, keyFunc func(*http.Request) string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
			if server.limiter == nil || limit <= 0 {
				next.ServeHTTP(response, request)
				return
			}

			key := keyFunc(request)
			allowed, remaining, retryAfter, err := server.limiter.Allow(request.Context(), key, limit, window)
			if err != nil {
				server.logger.Warn("rate limiter error, failing open", "key", key, "error", err)
				next.ServeHTTP(response, request)
				return
			}

			response.Header().Set("X-RateLimit-Limit", strconv.Itoa(limit))
			response.Header().Set("X-RateLimit-Remaining", strconv.Itoa(remaining))

			if !allowed {
				retrySeconds := int(math.Ceil(retryAfter.Seconds()))
				if retrySeconds <= 0 {
					retrySeconds = 1
				}
				response.Header().Set("Retry-After", strconv.Itoa(retrySeconds))
				writeJSON(response, http.StatusTooManyRequests, map[string]string{
					"error": "too many requests",
				})
				return
			}

			next.ServeHTTP(response, request)
		})
	}
}

func clientIP(request *http.Request) string {
	if forwarded := request.Header.Get("X-Forwarded-For"); forwarded != "" {
		if first, _, found := strings.Cut(forwarded, ","); found {
			return strings.TrimSpace(first)
		}
		return strings.TrimSpace(forwarded)
	}
	if realIP := request.Header.Get("X-Real-IP"); realIP != "" {
		return strings.TrimSpace(realIP)
	}
	host, _, err := net.SplitHostPort(request.RemoteAddr)
	if err == nil {
		return host
	}
	return request.RemoteAddr
}

func loginKey(request *http.Request) string {
	return fmt.Sprintf("ratelimit:login:%s", clientIP(request))
}

func reservationKey(request *http.Request) string {
	if subject, ok := subjectFromContext(request.Context()); ok {
		return fmt.Sprintf("ratelimit:reservation:%s", subject)
	}
	return fmt.Sprintf("ratelimit:reservation:%s", clientIP(request))
}
