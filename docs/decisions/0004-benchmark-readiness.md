# 4. Benchmark Readiness and High-Contention Verification

- Status: Accepted
- Date: 2026-11-23

## Context

Preparing OrderFlow for high-contention load testing (1,000 concurrent virtual users, open-arrival models) requires identifying and resolving architectural bottlenecks that could invalidate performance measurements or introduce measurement artifacts. Without controlled baselines, reproducibility controls, and strict invariants, benchmark results risk measuring testing harness artifacts (such as password hashing overhead or rate limit rejections) rather than actual system concurrency limits.

## Architectural Audit & Findings

### 1. Authentication & Password Hashing on the Hot Path
- **Finding:** The `/v1/auth/login` endpoint uses bcrypt with work factor 10, taking 50–100ms of CPU time per call. Under 1,000 concurrent users, running `/login` within the load test loop causes complete CPU exhaustion in bcrypt, hiding the true database throughput.
- **Resolution:** Implemented `cmd/seed` to pre-generate and persist 1,000 test users and pre-signed JWT bearer tokens into `loadtest/tokens.json`. Load testing tools (`k6` and `vegeta`) authenticate requests directly via HTTP `Authorization: Bearer <token>` headers, completely bypassing password hashing during benchmark runs.

### 2. Rate Limiting Throttling Concurrency
- **Finding:** Default rate limiting (10 reservations per minute per user, 5 logins per minute per IP) causes load tests to measure HTTP 429 rejections rather than transactional inventory contention.
- **Resolution:** Added feature flags (`ORDERFLOW_RATE_LIMIT_ENABLED`, `LOAD_TEST_MODE`) allowing rate limiting to be explicitly bypassed for baseline benchmark runs. Added safeguards preventing `LOAD_TEST_MODE=true` in `APP_ENV=production`.

### 3. Sold-Out Fast Path & Cache Correctness
- **Finding:** Short-circuiting sold-out checks in Redis provides substantial throughput gains, but false sold-out flags could incorrectly reject requests after inventory is released (due to payment rejection or hold expiry).
- **Resolution:** Verified and tested that both the workflow processor (on compensation/rejection) and the expiry sweeper actively invalidate the Redis key `event:<id>:sold_out`. Added `ORDERFLOW_CACHE_ENABLED` and `ORDERFLOW_SOLDOUT_FASTPATH` flags to allow controlled A/B comparison against pure PostgreSQL baselines.

### 4. Database Connection Pool & Goroutine Management
- **Finding:** Under 1,000 concurrent connections, default connection pool sizing may cause connection acquisition starvation. Singleflight is used to collapse concurrent read requests for identical event metadata and sold-out flags into a single inflight operation with explicit context cancellation isolation (`context.WithoutCancel`).
- **Follow-Up:** Database pool stats (`acquired_connections`, `idle_connections`, `max_connections`) are exported dynamically to Prometheus via `PgxPoolCollector` for continuous saturation monitoring.

### 5. Invariant Auditing
- **Finding:** Throughput and latency metrics are invalid if overselling occurred.
- **Resolution:** Built `cmd/verify` (`make verify`) to assert 5 SQL invariants after every run:
  1. `available >= 0` (zero overselling).
  2. `available + reserved + confirmed == initial capacity` across events and reservation rows.
  3. Exactly one terminal state transition per reservation.
  4. Outbox table fully drained (`published_at IS NOT NULL`).
  5. Inbox has no duplicate processed event IDs.

## Reproducibility & Environment Isolation

- Configured explicit Docker Compose resource limits (CPU quotas and memory ceilings) for all containers (`api: 1.0 CPU/512M`, `worker: 1.0 CPU/512M`, `postgres: 1.5 CPU/1024M`, `redis: 1.0 CPU/512M`, `redpanda: 1.0 CPU/1024M`).
- Created `cmd/capture_env` to automatically record hardware, OS, Go runtime, Git commit SHA, and active feature flags into `results/<run-id>/env.json`.

## Consequences

- Benchmark runs are strictly reproducible and controlled across both local and VM environments.
- Results compare true database row-level locking vs. Redis cache bypass rather than harness artifacts.
- Every benchmark run is validated by automated invariant checks before results are recorded.
