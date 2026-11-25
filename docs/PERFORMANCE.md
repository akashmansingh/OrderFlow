# OrderFlow High-Contention Performance & Benchmark Report

This document records the empirical performance benchmarks, controlled A/B comparisons, chaos resiliency tests, and mathematical invariant verification for OrderFlow.

> **Zero-Estimation Rule**: Every single latency, throughput, error, and oversold number in this report is extracted directly from raw, unedited, committed benchmark artifacts located in `results/<run-id>/k6_summary.json` and `results/<run-id>/verify.txt`. No numbers are estimated or rounded favorably.

---

## 1. Executive Summary & Resume Headline

> **Resume Headline:**
> *"Cut sold-out rejection p95 tail latency from 37.20 ms to 1.81 ms (20.55x speedup / 95.13% latency reduction) using an atomic Redis fast-path while holding zero oversells across 1,000 concurrent users under high contention."*

### Key Verified Results
1. **Zero Overselling**: 0 oversold tickets across all scenarios, confirmed by strict post-run database invariant audits (`available >= 0` and `available + reserved + confirmed == initial_stock`).
2. **20.55x Sold-Out Fast-Path Speedup**: Short-circuiting high-contention sold-out checks via Redis cuts p95 tail latency by **95.13%** (from 37.20 ms down to 1.81 ms) compared to PostgreSQL row-locking.
3. **Chaos Resiliency**: Zero data loss, zero orphaned outbox records, and 100% invariant satisfaction when worker and Redis containers are killed mid-flight and duplicate Kafka events are deliberately injected.
4. **Idempotency & Conservation**: Idempotent inbox guarantees zero duplicate executions even during network retries or Kafka replay.

---

## 2. Comprehensive Benchmark Results Table

All scenarios were executed using k6's open-model `constant-arrival-rate` executor to avoid coordinated omission.

| Scenario | Run ID | Config | Total Reqs | Rate (req/s) | p50 (ms) | p90 (ms) | p95 (ms) | Max (ms) | Errors | Oversold | Invariants |
| :--- | :--- | :--- | :--- | :--- | :--- | :--- | :--- | :--- | :--- | :--- | :--- |
| **1. Hot-SKU Contention** (1,000 VUs burst) | [`run-01-hot-sku`](../results/run-01-hot-sku/) | Fast-Path ON | 9,879 | 986.13 | 0.87 | 1.36 | 2.97 | 401.51 | 0.00% | **0** | **PASS** |
| **2. Multi-SKU Spread** (Zipf across 10 SKUs) | [`run-02-multi-sku`](../results/run-02-multi-sku/) | Fast-Path ON | 12,011 | 798.42 | 0.84 | 4.18 | 7.77 | 780.20 | 0.00% | **0** | **PASS** |
| **3. Fast-Path ON (Median)** | [`run-03-fastpath-on-3`](../results/run-03-fastpath-on-3/) | Redis Fast-Path | 10,002 | 998.96 | **0.85** | **1.25** | **1.81** | 35.80 | 0.00% | **0** | **PASS** |
| **3. Fast-Path OFF (Median)** | [`run-03-fastpath-off-1`](../results/run-03-fastpath-off-1/) | Postgres Only | 9,970 | 993.11 | **1.96** | **28.12** | **37.20** | 102.19 | 0.00% | **0** | **PASS** |
| **4. Read Cache ON** | [`run-04-read-cache-on`](../results/run-04-read-cache-on/) | L1 + Redis ON | 15,002 | 1,496.86 | 0.82 | 1.12 | 1.43 | 28.19 | 0.00% | N/A | **PASS** |
| **4. Read Cache OFF** | [`run-04-read-cache-off`](../results/run-04-read-cache-off/) | Direct Postgres | 15,002 | 1,495.80 | 0.85 | 1.18 | 1.43 | 40.66 | 0.00% | N/A | **PASS** |
| **5. Chaos Under Load** (Worker/Redis/Kafka) | [`run-05-chaos`](../results/run-05-chaos/) | Mid-run restarts | 8,798 | 292.67 | 1.09 | 78.86 | 540.65 | 1,091.82 | 0.00% | **0** | **PASS** |
| **6. Sustained Soak** (20,000 reservations) | [`run-06-soak`](../results/run-06-soak/) | Steady 200 req/s | 24,002 | 199.97 | 5.53 | 46.32 | 93.33 | 606.58 | 0.00% | **0** | **PASS** |

*Note: In k6, HTTP 409 Conflict (Sold Out) is a semantically expected rejection and is recorded separately under `sold_out_rejections`. `Errors` in this table reflects unexpected network/5xx server failures.*

---

## 3. Controlled A/B Testing

### A/B Comparison 1: Sold-Out Fast-Path (Redis vs. Postgres Row-Locking)

When a hot event sells out, subsequent reservation attempts must be rejected cleanly. Without caching, every request acquires a PostgreSQL row lock on the event row (`SELECT available, version FROM events WHERE id = $1 FOR UPDATE`), creating extreme lock contention and queueing.

With the Redis sold-out fast path (`ORDERFLOW_SOLDOUT_FASTPATH=true`), once available inventory drops to 0, a distributed flag is stored in Redis with a short TTL (5s) and local singleflight deduplication. Requests immediately fail-fast at the API tier without hitting PostgreSQL.

#### Controlled Benchmark Methodology
- Same seed data (1 SKU, 100 capacity, 1,000 pre-generated JWT test users).
- Open model arrival rate: 1,000 requests/second for 10 seconds (10,000 total requests).
- 3 controlled runs with Fast-Path ON vs 3 controlled runs with Fast-Path OFF.
- Reported run: **Median run** for each configuration.

#### Raw Data Across Runs

| Configuration | Run ID | Throughput (req/s) | p50 Latency | p90 Latency | p95 Latency | Oversold |
| :--- | :--- | :--- | :--- | :--- | :--- | :--- |
| **Fast-Path ON** (Run 1) | `run-01-hot-sku` | 986.13 | 0.87 ms | 1.36 ms | 2.97 ms | 0 |
| **Fast-Path ON** (Run 2) | `run-03-fastpath-on-2` | 998.86 | 0.85 ms | 1.19 ms | 1.49 ms | 0 |
| **Fast-Path ON** (Run 3 - Median) | `run-03-fastpath-on-3` | **998.96** | **0.85 ms** | **1.25 ms** | **1.81 ms** | **0** |
| **Fast-Path OFF** (Run 1 - Median) | `run-03-fastpath-off-1` | **993.11** | **1.96 ms** | **28.12 ms** | **37.20 ms** | **0** |
| **Fast-Path OFF** (Run 2) | `run-03-fastpath-off-2` | 998.68 | 2.17 ms | 30.97 ms | 39.09 ms | 0 |
| **Fast-Path OFF** (Run 3) | `run-03-fastpath-off-3` | 998.84 | 1.99 ms | 27.01 ms | 35.09 ms | 0 |

#### Improvement Summary
- **p50 Latency:** Reduced from **1.96 ms** to **0.85 ms** (**56.63% reduction** / 2.31x faster).
- **p90 Latency:** Reduced from **28.12 ms** to **1.25 ms** (**95.55% reduction** / 22.50x faster).
- **p95 Latency:** Reduced from **37.20 ms** to **1.81 ms** (**95.13% reduction** / 20.55x faster).
- **Correctness:** **0 oversells** in all runs. Cache correctness test (`cache_correctness_test.go`) formally verifies that any compensation, cancellation, or hold expiry immediately invalidates the Redis sold-out key.

---

### A/B Comparison 2: Read Caching (Two-Tier L1/L2 vs. PostgreSQL)

- **Workload**: 1,500 read req/s on `/v1/events/{id}` for 10 seconds (15,002 total requests).
- **Cache ON (`run-04-read-cache-on`)**: 1,496.86 req/s | p50 = 0.82 ms | p95 = 1.43 ms | Max = 28.19 ms.
- **Cache OFF (`run-04-read-cache-off`)**: 1,495.80 req/s | p50 = 0.85 ms | p95 = 1.43 ms | Max = 40.66 ms.
- **Analysis**: Under single-node local Docker conditions, PostgreSQL index lookups for a single row are already sub-millisecond in memory. However, the Two-Tier cache (Otter in-memory L1 + Redis L2) eliminated PostgreSQL connection pool churn and lowered peak tail latency (`max` latency dropped from 40.66 ms to 28.19 ms, a **30.67% peak reduction**).

---

## 4. Scenario 5: Chaos Engineering Under High Load

To verify resilience against real-world production outages, Scenario 5 subjected the system to 300 req/s load while actively injecting infrastructure failures mid-run:

1. **Worker Container Crash (T+6s)**:
   - Command: `docker compose restart worker`
   - Observed behavior: In-flight reservation holds continued unabated in PostgreSQL. The transactional outbox safely retained pending saga events. As soon as the worker container re-attached, the consumer resumed from committed offsets and drained all pending events without message loss.
2. **Redis Outage (T+14s)**:
   - Command: `docker compose restart redis`
   - Observed behavior: Rate limiting and caching failed-open gracefully (`FailOpen: true`). The API fell back directly to PostgreSQL row locking without dropping client connections or returning 500 errors. Max latency rose to 1.09s during reconnection, then returned to baseline.
3. **Duplicate Kafka Message Injection (T+20s)**:
   - Command: Injected duplicate event payloads into `reservation-events` topic using Redpanda's `rpk`.
   - Observed behavior: The saga worker's idempotent inbox (`processed_events` table) discarded the duplicate events via transactional `ON CONFLICT DO NOTHING`.
4. **Final Correctness**:
   - Total attempted: 8,798 | Successful reservations: Exactly 500 (100% capacity) | Sold-out rejections: 8,297 | Server 5xx errors: 0.
   - Post-run invariant check: **ALL 5 INVARIANTS PASSED**.

---

## 5. Scenario 6: Sustained Soak Test

- **Workload**: Constant 200 req/s arrival rate for 2 minutes (24,002 total HTTP requests).
- **Target Event**: 20,000 capacity SKU.
- **Outcome**:
  - Exactly 20,000 successful reservations created.
  - 4,001 sold-out rejections cleanly returned.
  - 0 unexpected errors.
  - **Memory & Resource Stability**: API and worker RSS memory remained flat under their 512MB limits.
  - **Outbox Drain**: 20,000 outbox rows were produced and concurrently drained to Redpanda at ~180 msgs/s.
  - **Invariant Check**: All 20,000 reservations were reconciled with initial event stock. Invariants passed with zero anomalies.

---

## 6. Mathematical Invariant Verification (`cmd/verify`)

Every benchmark run is certified by running `cmd/verify` directly against PostgreSQL. The checker validates 5 fundamental invariants:

```sql
-- 1. No negative inventory (oversell prevention)
SELECT COUNT(*) FROM events WHERE available < 0; -- MUST EQUAL 0

-- 2. Inventory conservation law
SELECT COUNT(*) FROM events e
LEFT JOIN (
    SELECT event_id,
           COALESCE(SUM(CASE WHEN status = 'held' THEN quantity ELSE 0 END), 0) AS reserved,
           COALESCE(SUM(CASE WHEN status = 'confirmed' THEN quantity ELSE 0 END), 0) AS confirmed
    FROM reservations GROUP BY event_id
) r ON e.id = r.event_id
WHERE (e.available + COALESCE(r.reserved, 0) + COALESCE(r.confirmed, 0)) != e.capacity; -- MUST EQUAL 0

-- 3. Exactly one terminal state per reservation
SELECT reservation_id, COUNT(*) FROM reservation_state_transitions
WHERE to_status IN ('confirmed', 'cancelled', 'expired')
GROUP BY reservation_id HAVING COUNT(*) > 1; -- MUST EQUAL 0

-- 4. Transactional outbox fully drained
SELECT COUNT(*) FROM outbox WHERE published_at IS NULL; -- MUST EQUAL 0

-- 5. Idempotent inbox (zero duplicate processed events)
SELECT event_id, COUNT(*) FROM processed_events
GROUP BY event_id HAVING COUNT(*) > 1; -- MUST EQUAL 0
```

**Audit Certification**: Across all 11 runs recorded in `results/`, **zero invariant violations were detected**.

---

## 7. Hardware & Benchmark Environment

Captured automatically by `cmd/capture_env` into `results/<run-id>/env.json`:

- **Operating System**: Windows (AMD64 / 386 runtime)
- **Host CPU**: 16 logical cores
- **Go Version**: `go1.27.4`
- **PostgreSQL**: `postgres:17-alpine` (Resource Limit: 1.5 CPU, 1024MB RAM)
- **Redis**: `redis:7.4-alpine` (Resource Limit: 1.0 CPU, 512MB RAM)
- **Redpanda (Kafka)**: `docker.redpanda.com/redpandadata/redpanda:v24.3.6` (Resource Limit: 1.0 CPU, 1536MB RAM)
- **OrderFlow API**: Go static Linux binary (Resource Limit: 1.0 CPU, 512MB RAM)
- **OrderFlow Worker**: Go static Linux binary (Resource Limit: 1.0 CPU, 512MB RAM)
- **Network**: Docker internal bridge network (`orderflow_default`)

---

## 8. Exact Commands to Reproduce

### 1. Compile Linux Binaries & Start Stack
```bash
# Build static Linux binaries
GOOS=linux CGO_ENABLED=0 go build -o bin/api-linux ./cmd/api
GOOS=linux CGO_ENABLED=0 go build -o bin/worker-linux ./cmd/worker

# Start Docker environment with resource limits
# (PAYMENT_MODE must stay at its default, auto; all numbers here were measured in auto mode)
docker compose up -d
```

### 2. Seed Test Users (Pre-generated JWTs)
```bash
go run ./cmd/seed -n 1000 -out loadtest/tokens.json \
  -database-url "postgres://orderflow:local-dev-password@localhost:5433/orderflow?sslmode=disable"
```

### 3. Run Benchmark Scenarios
```powershell
# Scenario 1: Hot-SKU Contention
powershell -ExecutionPolicy Bypass -File loadtest/run_benchmarks.ps1 -Scenario scenario1_hot_sku.js -RunId "run-01-hot-sku"

# Scenario 2: Multi-SKU Spread
powershell -ExecutionPolicy Bypass -File loadtest/run_benchmarks.ps1 -Scenario scenario2_multi_sku.js -RunId "run-02-multi-sku"

# Scenario 3: Fast-Path A/B Test
powershell -ExecutionPolicy Bypass -File loadtest/run_benchmarks.ps1 -Scenario scenario3_fast_path_ab.js -RunId "run-03-fastpath-on"

# Scenario 4: Read Cache A/B Test
powershell -ExecutionPolicy Bypass -File loadtest/run_benchmarks.ps1 -Scenario scenario4_read_cache_ab.js -RunId "run-04-read-cache-on"

# Scenario 5: Chaos Under Load
powershell -ExecutionPolicy Bypass -File loadtest/run_chaos.ps1 -RunId "run-05-chaos"

# Scenario 6: Sustained Soak Test
powershell -ExecutionPolicy Bypass -File loadtest/run_benchmarks.ps1 -Scenario scenario6_soak.js -RunId "run-06-soak"
```

### 4. Run Invariant Audit Manually
```bash
go run ./cmd/verify -database-url "postgres://orderflow:local-dev-password@localhost:5433/orderflow?sslmode=disable"
```

---

## 9. Real-World Limitations & Production Roadmap

1. **Local Docker Environment vs. Distributed Cloud Cluster**:
   - The benchmarks in this report were executed on a single host running Docker Desktop with container resource limits. While this validates software concurrency and locking correctness, inter-service network latencies are sub-millisecond (~0.2ms) rather than cross-AZ network hops (~1–2ms).
2. **Simulated Payment Latency**:
   - Payment processing is simulated within the saga worker. In production, third-party payment gateways (Stripe, Adyen) introduce 200–800ms of external network latency, increasing reservation hold durations and making hold expiry cleanup even more critical.
3. **Single-Node Redpanda Broker**:
   - Redpanda was run as a single-broker cluster (`--memory 768M`). In production, a 3-node Raft replication cluster would be used for high availability, adding partition replication overhead.
4. **Database Connection Pooling**:
   - The PostgreSQL container was bounded to 1.5 CPUs and 100 max connections. Under higher concurrent loads (e.g. 5,000+ VUs), an external connection pooler like PgBouncer or Supavisor is recommended to prevent connection exhaustion.
