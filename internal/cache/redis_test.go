package cache_test

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"sync/atomic"
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

func TestRedisClientConcurrentRateLimiting(t *testing.T) {
	server := miniredis.RunT(t)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	client, err := cache.New(server.Addr(), "", time.Second, logger)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer client.Close()

	ctx := context.Background()
	key := "test:concurrent:rate"
	limit := 10
	window := time.Minute
	numRequests := 50

	var wg sync.WaitGroup
	startBarrier := make(chan struct{})
	var allowedCount atomic.Int64
	var blockedCount atomic.Int64

	for i := 0; i < numRequests; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-startBarrier
			allowed, _, retryAfter, err := client.Allow(ctx, key, limit, window)
			if err != nil {
				t.Errorf("Allow() unexpected error = %v", err)
				return
			}
			if allowed {
				allowedCount.Add(1)
			} else {
				blockedCount.Add(1)
				if retryAfter <= 0 {
					t.Errorf("blocked request retryAfter <= 0: %v", retryAfter)
				}
			}
		}()
	}

	close(startBarrier)
	wg.Wait()

	if allowedCount.Load() != int64(limit) {
		t.Fatalf("allowedCount = %d, want %d", allowedCount.Load(), limit)
	}
	if blockedCount.Load() != int64(numRequests-limit) {
		t.Fatalf("blockedCount = %d, want %d", blockedCount.Load(), numRequests-limit)
	}
}

func TestRedisClientConcurrentSoldOutOperations(t *testing.T) {
	server := miniredis.RunT(t)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	client, err := cache.New(server.Addr(), "", time.Second, logger)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer client.Close()

	ctx := context.Background()
	eventID := "00000000-0000-0000-0000-000000000099"
	numGoroutines := 30
	var wg sync.WaitGroup

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			switch idx % 3 {
			case 0:
				_ = client.MarkSoldOut(ctx, eventID, 5*time.Second)
			case 1:
				_, _ = client.IsSoldOut(ctx, eventID)
			case 2:
				_ = client.ClearSoldOut(ctx, eventID)
			}
		}(i)
	}

	wg.Wait()
}

func TestRedisClientAllowDefensiveNilLogger(t *testing.T) {
	server := miniredis.RunT(t)

	// Explicitly pass nil logger: New must not panic and must fall back to default logger
	client, err := cache.New(server.Addr(), "", time.Second, nil)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer client.Close()

	allowed, rem, _, err := client.Allow(context.Background(), "test:nil:logger", 5, time.Minute)
	if err != nil || !allowed || rem != 4 {
		t.Fatalf("Allow() with nil logger: allowed=%v, remaining=%d, err=%v", allowed, rem, err)
	}
}

func TestRedisClientConcurrentFailOpen(t *testing.T) {
	server := miniredis.RunT(t)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	client, err := cache.New(server.Addr(), "", 100*time.Millisecond, logger)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	server.Close() // Disconnect Redis

	ctx := context.Background()
	numGoroutines := 30
	var wg sync.WaitGroup
	var failOpenCount atomic.Int64

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			allowed, remaining, _, err := client.Allow(ctx, fmt.Sprintf("test:failopen:%d", idx), 5, time.Minute)
			if err != nil && allowed && remaining == 5 {
				failOpenCount.Add(1)
			}
		}(i)
	}

	wg.Wait()

	if failOpenCount.Load() != int64(numGoroutines) {
		t.Fatalf("failOpenCount = %d, want %d", failOpenCount.Load(), numGoroutines)
	}
}
