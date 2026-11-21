package cache_test

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/theRiddler9/OrderFlow/internal/cache"
)

func TestRedisClientPingAndClose(t *testing.T) {
	server := miniredis.RunT(t)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	client, err := cache.New(server.Addr(), "", time.Second, logger)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer client.Close()

	if err := client.Ping(context.Background()); err != nil {
		t.Fatalf("Ping() error = %v", err)
	}
}

func TestRedisClientSoldOutOperations(t *testing.T) {
	server := miniredis.RunT(t)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	client, err := cache.New(server.Addr(), "", time.Second, logger)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer client.Close()

	ctx := context.Background()
	eventID := "00000000-0000-0000-0000-000000000001"

	// Initial state: not sold out
	soldOut, err := client.IsSoldOut(ctx, eventID)
	if err != nil {
		t.Fatalf("IsSoldOut() error = %v", err)
	}
	if soldOut {
		t.Fatalf("IsSoldOut() = true, want false")
	}

	// Mark sold out
	if err := client.MarkSoldOut(ctx, eventID, 5*time.Second); err != nil {
		t.Fatalf("MarkSoldOut() error = %v", err)
	}

	soldOut, err = client.IsSoldOut(ctx, eventID)
	if err != nil {
		t.Fatalf("IsSoldOut() error = %v", err)
	}
	if !soldOut {
		t.Fatalf("IsSoldOut() = false, want true")
	}

	// Clear sold out
	if err := client.ClearSoldOut(ctx, eventID); err != nil {
		t.Fatalf("ClearSoldOut() error = %v", err)
	}

	soldOut, err = client.IsSoldOut(ctx, eventID)
	if err != nil {
		t.Fatalf("IsSoldOut() error = %v", err)
	}
	if soldOut {
		t.Fatalf("IsSoldOut() = true, want false after clear")
	}
}

func TestRedisClientSoldOutExpiry(t *testing.T) {
	server := miniredis.RunT(t)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	client, err := cache.New(server.Addr(), "", time.Second, logger)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer client.Close()

	ctx := context.Background()
	eventID := "00000000-0000-0000-0000-000000000002"

	if err := client.MarkSoldOut(ctx, eventID, 2*time.Second); err != nil {
		t.Fatalf("MarkSoldOut() error = %v", err)
	}

	server.FastForward(3 * time.Second)

	soldOut, err := client.IsSoldOut(ctx, eventID)
	if err != nil {
		t.Fatalf("IsSoldOut() error = %v", err)
	}
	if soldOut {
		t.Fatalf("IsSoldOut() = true, want false after TTL elapsed")
	}
}

func TestRedisClientSlidingWindowRateLimiter(t *testing.T) {
	server := miniredis.RunT(t)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	client, err := cache.New(server.Addr(), "", time.Second, logger)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer client.Close()

	ctx := context.Background()
	key := "test:rate:user1"
	limit := 3
	window := time.Minute

	// Request 1: allowed, remaining 2
	allowed, remaining, _, err := client.Allow(ctx, key, limit, window)
	if err != nil || !allowed || remaining != 2 {
		t.Fatalf("req 1: allowed=%v, remaining=%d, err=%v", allowed, remaining, err)
	}

	// Request 2: allowed, remaining 1
	allowed, remaining, _, err = client.Allow(ctx, key, limit, window)
	if err != nil || !allowed || remaining != 1 {
		t.Fatalf("req 2: allowed=%v, remaining=%d, err=%v", allowed, remaining, err)
	}

	// Request 3: allowed, remaining 0
	allowed, remaining, _, err = client.Allow(ctx, key, limit, window)
	if err != nil || !allowed || remaining != 0 {
		t.Fatalf("req 3: allowed=%v, remaining=%d, err=%v", allowed, remaining, err)
	}

	// Request 4: blocked, remaining 0, retryAfter > 0
	allowed, remaining, retryAfter, err := client.Allow(ctx, key, limit, window)
	if err != nil || allowed || remaining != 0 || retryAfter <= 0 {
		t.Fatalf("req 4: allowed=%v, remaining=%d, retryAfter=%v, err=%v", allowed, remaining, retryAfter, err)
	}

	// Advance clock beyond window
	server.FastForward(61 * time.Second)

	// Request 5: allowed again
	allowed, remaining, _, err = client.Allow(ctx, key, limit, window)
	if err != nil || !allowed || remaining != 2 {
		t.Fatalf("req 5: allowed=%v, remaining=%d, err=%v", allowed, remaining, err)
	}
}

func TestRedisClientFailsOpenOnConnectionError(t *testing.T) {
	server := miniredis.RunT(t)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	client, err := cache.New(server.Addr(), "", time.Second, logger)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	// Close miniredis server to simulate Redis outage
	server.Close()

	ctx := context.Background()

	// Rate limiter must fail open (allowed=true, err!=nil)
	allowed, remaining, _, err := client.Allow(ctx, "test:key", 5, time.Minute)
	if err == nil {
		t.Fatal("expected error on disconnected Redis, got nil")
	}
	if !allowed || remaining != 5 {
		t.Fatalf("expected fail open: allowed=%v, remaining=%d", allowed, remaining)
	}

	// Sold out check must return false with error (delegates to PostgreSQL)
	soldOut, err := client.IsSoldOut(ctx, "00000000-0000-0000-0000-000000000003")
	if err == nil {
		t.Fatal("expected error on disconnected Redis, got nil")
	}
	if soldOut {
		t.Fatalf("expected soldOut=false on Redis error, got true")
	}
}
