package cache

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

// Client wraps a Redis client for rate limiting and inventory caching.
type Client struct {
	rdb     *redis.Client
	logger  *slog.Logger
	timeout time.Duration
}

// New creates and tests a Redis client connection.
func New(address, password string, timeout time.Duration, logger *slog.Logger) (*Client, error) {
	if timeout <= 0 {
		timeout = time.Second
	}
	rdb := redis.NewClient(&redis.Options{
		Addr:         address,
		Password:     password,
		DB:           0,
		DialTimeout:  timeout,
		ReadTimeout:  timeout,
		WriteTimeout: timeout,
	})

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	if err := rdb.Ping(ctx).Err(); err != nil {
		rdb.Close()
		return nil, fmt.Errorf("ping redis: %w", err)
	}

	return &Client{
		rdb:     rdb,
		logger:  logger,
		timeout: timeout,
	}, nil
}

// Close terminates the underlying Redis connections.
func (c *Client) Close() error {
	return c.rdb.Close()
}

// Ping checks if the Redis server is reachable.
func (c *Client) Ping(ctx context.Context) error {
	timeoutCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	return c.rdb.Ping(timeoutCtx).Err()
}

// IsSoldOut checks if an event is currently cached as sold out.
func (c *Client) IsSoldOut(ctx context.Context, eventID string) (bool, error) {
	timeoutCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	key := fmt.Sprintf("event:%s:sold_out", eventID)
	val, err := c.rdb.Get(timeoutCtx, key).Result()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return false, nil
		}
		c.logger.Warn("redis error on sold out check, falling back to database", "event_id", eventID, "error", err)
		return false, err
	}
	return val == "1", nil
}

// MarkSoldOut caches an event as sold out for the specified duration.
func (c *Client) MarkSoldOut(ctx context.Context, eventID string, ttl time.Duration) error {
	timeoutCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	key := fmt.Sprintf("event:%s:sold_out", eventID)
	if err := c.rdb.Set(timeoutCtx, key, "1", ttl).Err(); err != nil {
		c.logger.Warn("redis error marking event sold out", "event_id", eventID, "error", err)
		return err
	}
	return nil
}

// ClearSoldOut removes the sold out cache for an event.
func (c *Client) ClearSoldOut(ctx context.Context, eventID string) error {
	timeoutCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	key := fmt.Sprintf("event:%s:sold_out", eventID)
	if err := c.rdb.Del(timeoutCtx, key).Err(); err != nil {
		c.logger.Warn("redis error clearing sold out status", "event_id", eventID, "error", err)
		return err
	}
	return nil
}

// Allow executes a sliding-window rate limit check for a key.
// On Redis errors, it fails open: returns allowed=true and logs a warning.
func (c *Client) Allow(ctx context.Context, key string, limit int, window time.Duration) (bool, int, time.Duration, error) {
	timeoutCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	now := time.Now().UTC()
	nowMs := float64(now.UnixMilli())
	windowMs := float64(window.Milliseconds())
	clearBefore := nowMs - windowMs
	member := uuid.New().String()

	pipe := c.rdb.TxPipeline()
	pipe.ZRemRangeByScore(timeoutCtx, key, "-inf", fmt.Sprintf("%f", clearBefore))
	countCmd := pipe.ZCard(timeoutCtx, key)
	oldestCmd := pipe.ZRangeWithScores(timeoutCtx, key, 0, 0)
	pipe.Expire(timeoutCtx, key, window)
	_, err := pipe.Exec(timeoutCtx)
	if err != nil {
		c.logger.Warn("redis rate limiter failed, failing open", "key", key, "error", err)
		return true, limit, 0, err
	}

	count := countCmd.Val()
	if count >= int64(limit) {
		oldest := oldestCmd.Val()
		var retryAfter time.Duration
		if len(oldest) > 0 {
			elapsed := time.Duration(nowMs-oldest[0].Score) * time.Millisecond
			retryAfter = window - elapsed
			if retryAfter <= 0 {
				retryAfter = time.Second
			}
		} else {
			retryAfter = window
		}
		return false, 0, retryAfter, nil
	}

	pipe2 := c.rdb.TxPipeline()
	pipe2.ZAdd(timeoutCtx, key, redis.Z{Score: nowMs, Member: member})
	pipe2.Expire(timeoutCtx, key, window)
	if _, err := pipe2.Exec(timeoutCtx); err != nil {
		c.logger.Warn("redis rate limiter add entry failed, failing open", "key", key, "error", err)
		return true, limit - int(count), 0, err
	}

	remaining := limit - int(count) - 1
	if remaining < 0 {
		remaining = 0
	}
	return true, remaining, 0, nil
}
