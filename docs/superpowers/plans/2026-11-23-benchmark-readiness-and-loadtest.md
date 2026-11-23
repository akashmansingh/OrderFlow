# Stage 5.5 & Stage 6: Benchmark Readiness & Load Testing Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Establish scientific benchmark readiness (feature flags, pre-signed tokens, cache correctness, missing metrics, invariant checker, resource limits) and execute high-contention k6 load testing with reproducible raw results and `docs/PERFORMANCE.md`.

**Architecture:** Add configuration flags (`ORDERFLOW_CACHE_ENABLED`, `ORDERFLOW_SOLDOUT_FASTPATH`, `ORDERFLOW_RATE_LIMIT_ENABLED`, `LOAD_TEST_MODE`) to support baseline comparisons. Build `cmd/seed` to generate pre-authenticated JWTs and `cmd/verify` to validate database invariants post-run. Extend Prometheus metrics with partition consumer lag, saga latency, and pool statistics. Automate k6 scenarios in `loadtest/` and write raw results to `results/<run-id>/`.

**Tech Stack:** Go 1.27, k6 / vegeta, PostgreSQL 17, Redis 7.4, Redpanda, Docker Compose.

**Spec:** `docs/superpowers/specs/2026-11-23-benchmark-readiness-and-loadtest-design.md`

## Global Constraints
- Do not invent or estimate performance numbers anywhere; all documented figures must come from committed raw result files.
- PostgreSQL remains the sole authority for inventory allocation and state invariants.
- No public API or event schema may be modified without explicit confirmation.
- Work on branch `feat/benchmark-readiness-and-loadtest` with atomic, logical commits.

---

### Task 1: Configurable Feature Flags & Controlled Baseline Support

**Files:**
- Modify: `internal/config/config.go`
- Modify: `internal/config/config_test.go`
- Modify: `internal/httpapi/ratelimit.go`
- Modify: `internal/reservation/service.go`
- Modify: `cmd/api/main.go`
- Modify: `.env.example`
- Test: `internal/config/config_test.go`, `internal/reservation/service_test.go`, `internal/httpapi/ratelimit_test.go`

- [ ] Add `CacheEnabled`, `SoldOutFastPath`, `RateLimitEnabled`, and `LoadTestMode` fields to `internal/config/config.go` with environment parsing and defaults (`true`, `true`, `true`, `false`). Refuse `LoadTestMode` in production.
- [ ] Add unit tests in `internal/config/config_test.go` checking parsing, defaults, and production safeguard.
- [ ] Update `internal/reservation/service.go` so `isSoldOut` immediately returns `false` when `SoldOutFastPath` or `CacheEnabled` is disabled, falling through to PostgreSQL.
- [ ] Update `internal/httpapi/ratelimit.go` so requests bypass rate limiting when `RateLimitEnabled` is false.
- [ ] Run `go test -v ./internal/config/... ./internal/reservation/... ./internal/httpapi/...` to verify both ON and OFF paths.
- [ ] Commit with message: `Add benchmark feature flags and controlled baseline configuration`.

---

### Task 2: Load-Test Seeder & Pre-Signed Token Generator (`cmd/seed`)

**Files:**
- Create: `cmd/seed/main.go`
- Create: `internal/seed/seeder.go`
- Test: `internal/seed/seeder_test.go`

- [ ] Write unit test in `internal/seed/seeder_test.go` verifying generation of $N$ customer records and valid signed JWT tokens.
- [ ] Implement `internal/seed/seeder.go` creating users in PostgreSQL and signing tokens using `auth.TokenManager`.
- [ ] Implement `cmd/seed/main.go` supporting flags `-users=1000`, `-out=loadtest/tokens.json`, and `-event-capacity=100`.
- [ ] Test building `cmd/seed` and verify execution.
- [ ] Commit with message: `Add load-test seeder and pre-signed token generator CLI`.

---

### Task 3: Cache Correctness Audit & Stock Release Invalidation Proof

**Files:**
- Modify: `internal/workflow/processor.go`
- Modify: `internal/workflow/expiry.go`
- Create: `tests/integration/cache_correctness_test.go`

- [ ] Write integration test in `tests/integration/cache_correctness_test.go` that:
  1. Fills event inventory to capacity -> confirms `isSoldOut` returns `true`.
  2. Cancels or expires a reservation -> verifies sold-out key is cleared from Redis.
  3. Immediately attempts a new reservation -> confirms reservation succeeds with status `held` (proving no false sold-out rejections).
- [ ] Verify that compensation or rejection in `processor.go` and `expiry.go` clears sold-out key reliably.
- [ ] Run `go test -v ./tests/integration/... ./internal/workflow/...` and observe PASS.
- [ ] Commit with message: `Verify cache invalidation on stock release and add correctness test`.

---

### Task 4: Advanced Prometheus Metrics (Consumer Lag, Saga Duration, Pool Stats)

**Files:**
- Modify: `internal/metrics/metrics.go`
- Modify: `internal/metrics/metrics_test.go`
- Modify: `internal/workflow/processor.go`
- Modify: `cmd/worker/main.go`
- Modify: `cmd/api/main.go`

- [ ] Add `KafkaConsumerLag` gauge vector (`topic`, `partition`), `SagaDurationSeconds` histogram (buckets `0.001` to `5.0`), `OldestUnpublishedOutboxAgeSeconds` gauge, `DBPoolAcquiredConns`/`IdleConns`/`TotalConns` gauges, and `WorkerDLQEventsTotal` reason label to `internal/metrics/metrics.go`.
- [ ] Enable standard Go collectors (`prometheus.NewGoCollector()`, `prometheus.NewProcessCollector()`).
- [ ] In `cmd/worker/main.go`, compute Kafka consumer lag per partition and report to metrics.
- [ ] In `internal/workflow/processor.go`, observe `SagaDurationSeconds` using event `OccurredAt`.
- [ ] In `internal/outbox/repository.go` and `relay.go`, compute and report oldest unpublished outbox record age.
- [ ] Run `go test -v ./internal/metrics/... ./internal/workflow/... ./internal/outbox/... ./cmd/worker/...`.
- [ ] Commit with message: `Add consumer lag, saga latency, and connection pool metrics`.

---

### Task 5: Database Invariant Checker CLI (`cmd/verify`)

**Files:**
- Create: `internal/verify/invariants.go`
- Create: `internal/verify/invariants_test.go`
- Create: `cmd/verify/main.go`

- [ ] Write unit test in `internal/verify/invariants_test.go` checking SQL invariant queries against a test schema.
- [ ] Implement `internal/verify/invariants.go` running 5 core invariants:
  1. No oversell: `capacity - held_quantity - confirmed_quantity >= 0`.
  2. Conservation of stock: `available + held + confirmed == capacity`.
  3. Single terminal state per reservation, `expires_at IS NULL` when terminal.
  4. Outbox table fully drained (0 pending rows).
  5. Inbox has 0 duplicate message IDs processed.
- [ ] Implement `cmd/verify/main.go` connecting to database, running checks, printing a formatted summary table, and exiting code 0 or 1.
- [ ] Commit with message: `Add database invariant checker CLI`.

---

### Task 6: Docker Compose Resource Limits & Environment Capture (`results/<run-id>/env.json`)

**Files:**
- Modify: `compose.yaml`
- Create: `cmd/capture_env/main.go`
- Create: `docs/decisions/0004-benchmark-readiness.md`

- [ ] Add explicit CPU and memory resource limits in `compose.yaml` for all services (API, Worker, Postgres, Redis, Redpanda).
- [ ] Implement `cmd/capture_env/main.go` capturing CPU model, RAM, OS, Go/Postgres/Redis/Redpanda versions, Git SHA, and flags to `results/<run-id>/env.json`.
- [ ] Write architecture review findings and trade-offs into `docs/decisions/0004-benchmark-readiness.md`.
- [ ] Commit with message: `Configure compose resource limits, env capture tool, and ADR 0004`.

---

### Task 7: High-Contention Load Testing Scripts (`loadtest/`)

**Files:**
- Create: `loadtest/scenario1_hot_sku.js`
- Create: `loadtest/scenario2_multi_sku.js`
- Create: `loadtest/scenario3_fastpath_ab.js`
- Create: `loadtest/scenario4_read_cache_ab.js`
- Create: `loadtest/scenario5_chaos.js`
- Create: `loadtest/scenario6_soak.js`
- Create: `loadtest/run.ps1` (automated runner)

- [ ] Implement Scenario 1: 1,000 concurrent VUs contesting 1 SKU with 100 units using k6 `constant-arrival-rate`.
- [ ] Implement Scenario 2: Zipf-like multi-SKU distribution.
- [ ] Implement Scenario 3: Fast-path A/B (3 runs ON vs 3 runs OFF, recording median).
- [ ] Implement Scenario 4: Read-heavy cache A/B.
- [ ] Implement Scenario 5: Chaos under load (kill worker, kill Redis, duplicate messages, assert `cmd/verify` passes).
- [ ] Implement Scenario 6: 15-minute steady soak test monitoring memory and backlog.
- [ ] Implement runner script orchestrating runs, saving raw results into `results/<run-id>/`, and executing `cmd/verify`.
- [ ] Commit with message: `Add comprehensive k6 load-testing scenarios and runner`.

---

### Task 8: Benchmark Execution, Verification & PERFORMANCE.md

**Files:**
- Create: `results/<run-id>/*`
- Create: `docs/PERFORMANCE.md`
- Modify: `README.md`

- [ ] Execute Scenario 1 (Hot-SKU contention) and verify `cmd/verify` passes with 0 oversell.
- [ ] Execute Scenario 3 (Fast-path A/B) with 3 runs ON and 3 runs OFF, capturing raw outputs in `results/`.
- [ ] Generate `docs/PERFORMANCE.md` containing the results table (Req/s, p50, p95, p99, Errors, Oversold), percentage differences, exact commands, hardware environment, and limitations.
- [ ] Update `README.md` with headline performance summary.
- [ ] Commit with message: `Document benchmark results and performance report`.
