# Stage 5.5 & Stage 6: Benchmark Readiness & High-Contention Load Testing Design

**Date:** 2026-11-23  
**Status:** Accepted  
**Branch:** `feat/benchmark-readiness-and-loadtest`  
**Authors:** OrderFlow Team  

## 1. Context & Purpose

OrderFlow has established core transactional reservation semantics, asynchronous outbox workflow processing, Redis caching/rate limiting, and Prometheus metrics with DLQ routing. 

Before claiming quantitative throughput or latency improvements (e.g., "reduced p95 latency by X% using Redis fast-path"), we must establish **controlled, scientifically reproducible benchmark readiness (Stage 5.5)**, followed by **rigorous open-model and constant-arrival-rate load testing (Stage 6)**. Every number published in documentation must stem directly from committed raw result files under `results/<run-id>/`.

---

## 2. Part A: Stage 5.5 Benchmark Readiness Subsystems

```text
                                [Environment: results/<run-id>/env.json]
                                [Resource Limits: compose.yaml]
                                                   │
┌───────────────────────────────┐                  ▼
│ Feature Flags & Config        │       ┌──────────────────────┐
│ • ORDERFLOW_CACHE_ENABLED     ├──────►│   OrderFlow Stack    │
│ • ORDERFLOW_SOLDOUT_FASTPATH  │       │ (API, Worker, DB,    │
│ • ORDERFLOW_RATE_LIMIT_ENABLED│       │  Redis, Redpanda)    │
│ • LOAD_TEST_MODE              │       └──────────┬───────────┘
└──────────────┬────────────────┘                  │
               │ Pre-signed Tokens                 ▼
               ▼                         [Prometheus /metrics]
       ┌───────────────┐                 • saga latency histogram
       │   cmd/seed    ├─► tokens.json   • partition consumer lag
       └───────────────┘                 • outbox oldest row age
               │                         • pgx pool statistics
               ▼
┌──────────────────────────────┐                   │
│      k6 / vegeta Load        ├───────────────────┤
│ (1000 VUs / Open-model rate) │                   │
└──────────────┬───────────────┘                   │
               │                                   │
               ▼                                   ▼
┌──────────────────────────────┐         ┌─────────────────────┐
│  Invariant Checker:          │         │ results/<run-id>/   │
│  cmd/verify                  │◄────────┤ • raw k6 JSON       │
│ • available >= 0             │         │ • env.json          │
│ • available+held+conf==total │         │ • verify.txt        │
│ • zero duplicate events      │         │ • metrics snapshot  │
│ • outbox fully drained       │         └─────────────────────┘
└──────────────────────────────┘
```

### 1. Feature Flags & Controlled Baselines
To produce valid before/after comparisons, the same binary must support running in baseline vs optimized configurations without code changes:
- `ORDERFLOW_CACHE_ENABLED` (bool, default `true`): Disables Redis interactions entirely when `false`.
- `ORDERFLOW_SOLDOUT_FASTPATH` (bool, default `true`): When `false`, forces all reservation requests to query and lock PostgreSQL directly, establishing the true database contention baseline.
- `ORDERFLOW_RATE_LIMIT_ENABLED` (bool, default `true`): When `false`, disables rate limit rejections for raw capacity benchmarks.
- `LOAD_TEST_MODE` (bool, default `false`): Requires explicit activation; raises or bypasses rate limits for pre-seeded test users while preventing accidental activation in production (`APP_ENV=production` refuses to start if `LOAD_TEST_MODE=true`).

### 2. Token Pre-Generation (`cmd/seed`)
Benchmarking reservation capacity must not be contaminated by `bcrypt` password hashing on `/v1/auth/login`. A dedicated CLI `cmd/seed`:
- Connects to PostgreSQL and seeds $N$ test customer accounts.
- Pre-signs valid JWTs with `jwt.NewWithClaims` using the configured `JWT_SECRET`.
- Emits `loadtest/tokens.json` formatted for k6 (`["Bearer ey...", ...]`).

### 3. Cache Correctness & Invalidation Audit
PostgreSQL is authoritative. The negative cache key (`event:<id>:sold_out`) must never cause a false rejection when capacity exists.
- Invalidation must trigger on:
  1. Hold cancellation (payment rejection).
  2. Hold expiry (sweeper cleanup).
  3. Compensation release.
- A dedicated test suite will prove that releasing 1 unit from a sold-out event immediately permits a subsequent reservation without waiting for key TTL expiry.

### 4. Advanced Telemetry & Missing Metrics
- `orderflow_kafka_consumer_lag{topic, partition}`: Real-time difference between high watermark and consumer group committed offset.
- `orderflow_saga_duration_seconds`: Histogram (buckets `0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5`) measuring elapsed duration from reservation creation to terminal transition.
- `orderflow_outbox_oldest_record_age_seconds`: Gauge measuring `EXTRACT(EPOCH FROM (NOW() - MIN(occurred_at)))` for pending outbox records.
- `orderflow_db_pool_acquired_conns`, `orderflow_db_pool_idle_conns`, `orderflow_db_pool_total_conns`: Dynamic stats from `pgxpool.Stat()`.
- `orderflow_worker_dlq_events_total{reason}`: Counter partitioned by failure classification.
- Standard Go runtime collectors enabled.

### 5. Invariant Checker (`cmd/verify`)
Post-run database consistency verifier exiting non-zero if any invariant fails:
1. **No Oversell:** `capacity - held_quantity - confirmed_quantity >= 0` for all events.
2. **Conservation of Stock:** `available + held + confirmed == capacity` for every event.
3. **State Integrity:** Every reservation has exactly 1 valid state; terminal states have `expires_at IS NULL`.
4. **Outbox Drained:** Zero uncommitted or unpublished outbox rows remain.
5. **Inbox Deduplication:** Zero duplicate message IDs in `processed_events`.

### 6. Controlled Container Limits & Environment Capture
- `compose.yaml`: Set explicit CPU and memory resource limits on all services (API, Worker, Postgres, Redis, Redpanda).
- `scripts/capture_env`: Captures OS, CPU, RAM, Go version, Git commit SHA, and active flags into `results/<run-id>/env.json`.

---

## 3. Part B: Stage 6 Load Testing Scenarios

All scenarios are automated in `loadtest/` using k6's `constant-arrival-rate` executor (open model to prevent coordinated omission):
1. **Scenario 1 (Hot-SKU Contention):** 1,000 concurrent VUs requesting tickets for a single event with 100 units. Assert exactly 100 succeed, 900 rejected with 409 Conflict, 0 oversold, `cmd/verify` passes.
2. **Scenario 2 (Multi-SKU Spread):** Realistic Zipfian distribution across 50 events.
3. **Scenario 3 (Fast-Path A/B):** Scenario 1 repeated 3 times with Fast-Path ON vs 3 times with Fast-Path OFF. Report median run with p50, p95, p99, and throughput.
4. **Scenario 4 (Read-Heavy Cache A/B):** Read availability requests with Redis cache ON vs OFF.
5. **Scenario 5 (Chaos Injection):** Mid-run worker restart, Redis kill, and duplicate Kafka message delivery. Invariants must strictly pass after recovery.
6. **Scenario 6 (Soak Test):** 15-minute steady load testing for memory leaks and consumer lag creep.

Documentation deliverable: `docs/PERFORMANCE.md` generated purely from raw `results/<run-id>/` files.
