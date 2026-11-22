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

// New creates and tests a Redis client connection with tuned pool settings.
func New(address, password string, timeout time.Duration, logger *slog.Logger) (*Client, error) {
	if timeout <= 0 {
		timeout = time.Second
	}
	rdb := redis.NewClient(&redis.Options{
		Addr:         address,
		Password:     password,
		DB:           0,
		PoolSize:     50,
		MinIdleConns: 10,
		MaxRetries:   1,
		DialTimeout:  500 * time.Millisecond,
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

var slidingWindowRateLimitScript = redis.NewScript(`
local key = KEYS[1]
local now = tonumber(ARGV[1])
local window = tonumber(ARGV[2])
local limit = tonumber(ARGV[3])
local member = ARGV[4]

local clearBefore = now - window
redis.call('ZREMRANGEBYSCORE', key, '-inf', clearBefore)

local count = redis.call('ZCARD', key)

if count >= limit then
    local oldest = redis.call('ZRANGE', key, 0, 0, 'WITHSCORES')
    local retryAfter = 0
    if oldest and #oldest >= 2 then
        local oldestScore = tonumber(oldest[2])
        local elapsed = now - oldestScore
        retryAfter = window - elapsed
        if retryAfter <= 0 then
            retryAfter = 1000
        end
    else
        retryAfter = window
    end
    return {0, 0, retryAfter}
end

redis.call('ZADD', key, now, member)
local ttlSeconds = math.ceil(window / 1000)
redis.call('EXPIRE', key, ttlSeconds)

local remaining = limit - count - 1
if remaining < 0 then
    remaining = 0
end

return {1, remaining, 0}
`)

// Allow executes an atomic sliding-window rate limit check using a server-side Lua script.
// On Redis errors, it fails open: returns allowed=true and logs a warning.
func (c *Client) Allow(ctx context.Context, key string, limit int, window time.Duration) (bool, int, time.Duration, error) {
	timeoutCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	now := time.Now().UTC()
	nowMs := now.UnixMilli()
	windowMs := window.Milliseconds()
	member := uuid.New().String()

	res, err := slidingWindowRateLimitScript.Run(
		timeoutCtx,
		c.rdb,
		[]string{key},
		nowMs,
		windowMs,
		limit,
		member,
	).Slice()
	if err != nil {
		c.logger.Warn("redis rate limiter script failed, failing open", "key", key, "error", err)
		return true, limit, 0, err
	}

	allowedVal, _ := res[0].(int64)
	remainingVal, _ := res[1].(int64)
	retryAfterMsVal, _ := res[2].(int64)

	allowed := allowedVal == 1
	remaining := int(remainingVal)
	retryAfter := time.Duration(retryAfterMsVal) * time.Millisecond
	return allowed, remaining, retryAfter, nil
}
