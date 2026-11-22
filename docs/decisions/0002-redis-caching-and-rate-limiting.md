# 2. Redis Caching and Rate Limiting

- Status: Accepted
- Date: 2026-11-22

## Context

High-contention ticket reservations subject the backend to burst traffic on both public endpoints (brute-force login attempts and ticket bot bursts) and sold-out inventory lookups. Relying exclusively on PostgreSQL row-level locks under severe contention can saturate connection pools and increase tail latency. However, PostgreSQL must remain the sole authoritative source of truth for actual inventory commitments.

## Decision

Use Redis exclusively as an auxiliary acceleration and protection tier with explicit fail-open and graceful degradation semantics:
1. **Sliding-Window Rate Limiting:** Enforce sliding-window limits using Redis sorted set pipelines on `POST /v1/auth/login` (by IP, default 5 req/min) and `POST /v1/events/{id}/reservations` (by user subject or IP, default 10 req/min). Return `429 Too Many Requests` with standard `X-RateLimit-*` and `Retry-After` headers.
2. **Fast-Path Sold-Out Cache:** Cache sold-out status in Redis with a short TTL (5 seconds) upon observing `ErrInsufficientInventory` from PostgreSQL. Requests hitting a sold-out event fast-fail with `409 Conflict` without acquiring a database transaction.
3. **Cache Invalidation:** The workflow worker actively clears the sold-out flag in Redis whenever an expired or rejected reservation releases held inventory.
4. **Resilience & Fallback:** If Redis is down, unreachable, or times out, the rate limiter fails open (allowing traffic and logging a warning) and the reservation service proceeds directly to PostgreSQL. Redis is never used to confirm availability.

## Consequences

- The API sheds load from database connection pools during sold-out surges.
- Rate limiting protects authentication and booking endpoints from brute force and runaway bursts.
- Even during a complete Redis outage, ticket reservations and logins continue safely through PostgreSQL without service disruption.
- False positive sold-out flags expire automatically within 5 seconds even if worker cache invalidation fails.
