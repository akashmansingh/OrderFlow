# Redis Caching and Rate Limiting Design

## Intent

Integrate Redis into OrderFlow as an auxiliary acceleration and protection tier without compromising PostgreSQL's position as the authoritative source of truth. Redis will provide HTTP endpoint rate limiting (brute force protection on authentication and request throttling on ticket reservations) and fast-path sold-out inventory caching to shed database load under contention.

## Approaches considered

1. **Redis sliding window with atomic pipeline and fast-path sold-out cache — selected.** Uses Redis sorted sets (`ZADD`, `ZREMRANGEBYSCORE`, `ZCARD`) for accurate sliding-window rate limiting. Uses simple string keys with short TTLs for sold-out events. On any Redis error or timeout, the system fails open for rate limiting and falls back to PostgreSQL for reservations.
2. **Fixed-window rate limiting in Redis.** Simpler (`INCR` with `EXPIRE`), but susceptible to 2x burst traffic across window boundaries.
3. **In-memory Go rate limiting without Redis.** Fast and simple, but does not coordinate across multiple API instances and loses state on process restart.

## Selected design

### Connection and resilience

- Add `internal/cache/redis.go` using `github.com/redis/go-redis/v9`.
- Configuration:
  - `REDIS_ADDRESS`: default `127.0.0.1:6379` (local) or `redis:6379` (Docker Compose).
  - `REDIS_PASSWORD`: optional, default empty.
  - `REDIS_TIMEOUT`: default `1s`.
  - `RATE_LIMIT_LOGIN_PER_MINUTE`: default `5`.
  - `RATE_LIMIT_RESERVATION_PER_MINUTE`: default `10`.
  - `CACHE_SOLD_OUT_TTL`: default `5s`.
- **Resilience / Fallback Rule:** Redis is disposable and non-authoritative. If Redis is unavailable or times out:
  - Rate limiting logs a warning and allows the request through (fail-open) so legitimate users are not blocked by a cache outage.
  - Sold-out check logs a warning and delegates directly to the PostgreSQL transaction.

### Rate limiting middleware

- Key scheme:
  - Login: `ratelimit:login:{ip}`
  - Reservations: `ratelimit:reservation:{user_id_or_ip}` (uses authenticated token subject UUID if available; falls back to client IP).
- Mechanism:
  - Sliding-window algorithm using a Redis sorted set:
    1. Score = current timestamp in milliseconds.
    2. Remove entries older than `now - window_size`.
    3. Count remaining entries in window.
    4. If count >= limit, reject with `429 Too Many Requests`.
    5. Otherwise, add current request and set TTL to window size.
  - HTTP response headers:
    - `X-RateLimit-Limit`: configured limit.
    - `X-RateLimit-Remaining`: available quota in current window.
    - `Retry-After`: seconds until the oldest request in the window expires (on 429).

### Fast-path sold-out inventory cache

- Key scheme: `event:{event_id}:sold_out`
- Behavior:
  - On reservation request (`POST /v1/events/{id}/reservations`):
    - Check Redis `event:{event_id}:sold_out`. If key exists (`"1"`), immediately return `409 Conflict` (insufficient inventory) without acquiring a PostgreSQL database connection or row lock.
    - If key does not exist or Redis check fails, proceed to PostgreSQL transaction.
    - When PostgreSQL returns `ErrInsufficientInventory`, set `event:{event_id}:sold_out = "1"` with a short TTL (`CACHE_SOLD_OUT_TTL`, e.g., 5 seconds).
  - Cache Invalidation:
    - When the workflow worker cancels an expired or rejected reservation, it deletes or expires the `event:{event_id}:sold_out` key in Redis so new reservation attempts can immediately claim the released tickets.

## Failure guarantees

- PostgreSQL remains the sole authority for inventory, reservation states, and idempotency. Redis is never used to confirm availability.
- A false positive in the fast-path cache expires within `CACHE_SOLD_OUT_TTL` (5s), after which PostgreSQL re-evaluates.
- Redis failures never cause `500 Internal Server Error` on public endpoints; the system degrades gracefully to PostgreSQL.

## Verification

- Unit tests for sliding-window rate limiter using `alicebob/miniredis/v2` or interface abstraction.
- Unit tests asserting `X-RateLimit-*` and `Retry-After` headers and `429` status code.
- Unit tests asserting fail-open behavior when Redis errors out.
- Unit tests asserting fast-path rejection on cached sold-out events and cache clearing on reservation expiry/cancellation.

## Non-goals

- Distributed locks in Redis (Redlock) — PostgreSQL row-level locks handle all transactional concurrency.
- Redis cluster or sentinel topology (single VM portfolio scope).
- Persistent user sessions or token revocation in Redis.
