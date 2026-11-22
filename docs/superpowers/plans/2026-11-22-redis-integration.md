# Redis Caching and Rate Limiting Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Integrate Redis for sliding-window HTTP rate limiting and fast-path sold-out inventory caching while preserving PostgreSQL as the single authoritative source of truth.

**Architecture:** A Redis client connection pool is shared with the HTTP server and workflow worker. HTTP middleware enforces per-IP login limits and per-user/IP reservation limits using Redis sorted sets with automatic fail-open on Redis error. The reservation service consults a fast-path sold-out key before initiating PostgreSQL transactions and writes it when an event sells out; the worker invalidates this key whenever held capacity is released.

**Tech Stack:** Go 1.27, `github.com/redis/go-redis/v9`, `alicebob/miniredis/v2` (for tests), PostgreSQL 17, Docker Compose.

**Spec:** `docs/superpowers/specs/2026-11-22-redis-integration-design.md`

## Global Constraints

- PostgreSQL remains the sole authority for inventory allocation and confirmed reservations. Redis is never used to confirm availability.
- On any Redis connection error or timeout, the system must fail open (for rate limits) or fall back to PostgreSQL (for reservations) without throwing 500 errors to clients.
- Rate limit headers (`X-RateLimit-Limit`, `X-RateLimit-Remaining`, `Retry-After`) must be returned on rate-limited endpoints.
- Commit every changed file separately with an imperative, scoped message per repository guidelines.

## Review Focus

- Rate limiter must properly return 429 when threshold is reached within window and 200 after window slides.
- Rate limiter must fail open when Redis client errors out or is disconnected.
- Sold-out fast path must return 409 Conflict without executing a PostgreSQL query when the sold-out flag is present in Redis.
- Worker expiry/cancellation must clear the sold-out key when held inventory is released.

---

### Task 1: Redis client, configuration, and resilience helper

**Files:**
- Modify: `go.mod`, `go.sum`
- Modify: `internal/config/config.go`, `internal/config/config_test.go`
- Create: `internal/cache/redis.go`, `internal/cache/redis_test.go`

**Interfaces:**
- Produces `config.Config` fields: `RedisAddress string`, `RedisPassword string`, `RedisTimeout time.Duration`, `RateLimitLoginPerMinute int`, `RateLimitReservationPerMinute int`, `CacheSoldOutTTL time.Duration`.
- Produces `cache.Client` interface and constructor `cache.NewRedisClient(cfg config.Config, logger *slog.Logger) (*cache.Client, error)`.

- [ ] Write failing test in `internal/cache/redis_test.go` and `internal/config/config_test.go` for Redis client connection and configuration parsing.
- [ ] Run `go test ./internal/cache ./internal/config -v` and observe failure.
- [ ] Add `github.com/redis/go-redis/v9` to `go.mod`.
- [ ] Implement `internal/config/config.go` fields and defaults (`REDIS_ADDRESS` default `127.0.0.1:6379`, `REDIS_TIMEOUT` default `1s`, etc.).
- [ ] Implement `internal/cache/redis.go` with health check and graceful ping.
- [ ] Run `go test ./internal/cache ./internal/config -v` and confirm tests pass.
- [ ] Commit files separately with scoped imperative messages.

---

### Task 2: Sliding-window rate limiter middleware

**Files:**
- Create: `internal/httpapi/ratelimit.go`, `internal/httpapi/ratelimit_test.go`
- Modify: `internal/httpapi/server.go`
- Modify: `cmd/api/main.go`

**Interfaces:**
- Produces `httpapi.RateLimiter` middleware taking limit, window, key generator func, and logger.
- Injects rate limiting onto `POST /v1/auth/login` and `POST /v1/events/{id}/reservations`.

- [ ] Write failing unit test in `internal/httpapi/ratelimit_test.go` asserting sliding window enforcement, `429` status, `X-RateLimit-*` headers, and fail-open on Redis error.
- [ ] Run `go test ./internal/httpapi -run TestRateLimit -v` and observe failure.
- [ ] Implement sliding window log in `internal/httpapi/ratelimit.go` using Redis pipeline (`ZREMRANGEBYSCORE`, `ZCARD`, `ZADD`, `EXPIRE`).
- [ ] Wire rate limiting into `internal/httpapi/server.go` and initialize Redis client in `cmd/api/main.go`.
- [ ] Run `go test ./internal/httpapi -v` and confirm all tests pass.
- [ ] Commit files separately with scoped imperative messages.

---

### Task 3: Fast-path sold-out cache and worker invalidation

**Files:**
- Modify: `internal/reservation/service.go`
- Modify: `internal/workflow/processor.go`
- Modify: `internal/workflow/expiry.go`
- Modify: `cmd/worker/main.go`
- Create/Modify tests in `internal/reservation` and `internal/workflow`

**Interfaces:**
- Produces `cache.InventoryCache` interface with `IsSoldOut(ctx context.Context, eventID string) (bool, error)`, `MarkSoldOut(ctx context.Context, eventID string, ttl time.Duration) error`, `ClearSoldOut(ctx context.Context, eventID string) error`.
- Wires fast-path check into `reservation.Service.CreateReservation` and cache invalidation into `workflow.Processor` and `workflow.Expiry`.

- [ ] Write failing tests for fast-path sold out rejection and cache invalidation on ticket release.
- [ ] Run tests and observe failure.
- [ ] Implement `IsSoldOut` / `MarkSoldOut` / `ClearSoldOut` in cache layer and connect to `reservation.Service`.
- [ ] Connect cache invalidation when workflow processor cancels a rejected hold and when expiry worker releases expired holds.
- [ ] Run all tests in `internal/reservation/...` and `internal/workflow/...` and confirm pass.
- [ ] Commit files separately with scoped imperative messages.

---

### Task 4: ADR, Compose, and Documentation

**Files:**
- Create: `docs/decisions/0002-redis-caching-and-rate-limiting.md`
- Modify: `compose.yaml`
- Modify: `.env.example`
- Modify: `docs/local-development.md`
- Modify: `README.md`

**Interfaces:**
- Documents ADR 0002, Compose environment variables, local testing commands, and fallback behavior.

- [ ] Create `docs/decisions/0002-redis-caching-and-rate-limiting.md`.
- [ ] Update `compose.yaml`, `.env.example`, `docs/local-development.md`, and `README.md`.
- [ ] Run full project verification: `gofmt -l .`, `go vet ./...`, `go test ./cmd/... ./internal/...`, `go build ./cmd/api`, `go build ./cmd/worker`.
- [ ] Commit each modified document separately.
