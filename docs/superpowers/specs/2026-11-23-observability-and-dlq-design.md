# Stage 5: Observability, Prometheus Metrics & Dead-Letter Queue Design

**Date:** 2026-11-23  
**Status:** Accepted  
**Authors:** OrderFlow Team  

## 1. Context and Goals

OrderFlow is designed to demonstrate production-grade reliability, reproducible operational evidence, and resilient failure recovery for high-contention ticket reservations.

In Stages 1-4, we built:
1. PostgreSQL row-level locking for inventory allocation and transactional outbox.
2. Background workflow worker consuming from Redpanda, simulating payments and expiring held reservations.
3. JWT-based authentication and user-scoped reservation ownership.
4. Redis-backed atomic sliding-window rate limiting and singleflight negative inventory caching.

Stage 5 introduces the operational observability and resilience guarantees required before running high-contention benchmarks:
1. **Prometheus Metrics (`/metrics`):** Standard Prometheus telemetry covering API latency, throughput, rate limit rejections, Redis cache hit/miss/fail-open rates, outbox backlog, and asynchronous worker outcomes.
2. **Dead-Letter Queue (DLQ) & Poison Message Resilience:** Graceful handling of unparseable, malformed, or poisoned Redpanda messages via retries with exponential backoff and forwarding to a dedicated `reservation-events-dlq` topic, preventing consumer partition stalls.
3. **Workflow Normalization:** Normalizing the simulated payment approval state to canonical `"confirmed"` (aligning with `reservation.confirmed.v1` and `docs/architecture.md`).

---

## 2. Architecture & Subsystems

```text
[Client Request]
       │
       ▼
[HTTP Middleware: Metrics & Rate Limit] ──► Records latency, status, rate limit rejections
       │
       ├──► /metrics (Prometheus scrape endpoint)
       ▼
[Reservation API] ──► PostgreSQL (Outbox records)
                           │
                           ▼
                     [Outbox Relay] ──► Updates outbox backlog gauge
                           │
                           ▼
                    [Redpanda Topic: reservation-events]
                           │
                           ▼
                  [Workflow Worker Consumer]
                           │
                 ┌─────────┴─────────┐
                 │ Valid Event       │ Poison / Unrecoverable Event
                 ▼                   ▼
        [Processor / Expiry]   [DLQ Publisher]
                 │                   │
                 ▼                   ▼
           PostgreSQL / Redis  [Redpanda Topic: reservation-events-dlq]
```

### A. Metrics Subsystem (`internal/metrics`)

A dedicated `internal/metrics` package registers and manages Prometheus metrics:

1. **HTTP Metrics:**
   - `orderflow_http_requests_total{method, path, status}` (Counter)
   - `orderflow_http_request_duration_seconds{method, path}` (Histogram: buckets [0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5])
   - `orderflow_ratelimit_rejected_total{endpoint}` (Counter)

2. **Cache & Rate Limiting Metrics:**
   - `orderflow_redis_cache_hits_total{cache}` (Counter)
   - `orderflow_redis_cache_misses_total{cache}` (Counter)
   - `orderflow_redis_failopen_total{operation}` (Counter)

3. **Outbox & Worker Metrics:**
   - `orderflow_outbox_backlog_records` (Gauge)
   - `orderflow_worker_events_processed_total{event_type, outcome}` (Counter: outcomes `confirmed`, `cancelled`, `expired`, `dlq`, `ignored`)
   - `orderflow_worker_event_duration_seconds{event_type}` (Histogram)
   - `orderflow_worker_retries_total{event_type}` (Counter)
   - `orderflow_worker_dlq_events_total{event_type}` (Counter)

4. **Scrape Endpoints:**
   - Public `GET /metrics` on the API server.
   - Dedicated internal metrics server (or `/metrics` endpoint) on the Worker process for scraper aggregation.

### B. Dead-Letter Queue (DLQ) & Poison Message Recovery

1. **Failure Classification:**
   - **Poison / Non-retryable Errors:** JSON decoding failure, missing mandatory fields (e.g. invalid UUIDs), unsupported schema versions. Retrying will never succeed.
   - **Transient Errors:** Database connection drop, temporary lock acquisition timeout. These should be retried up to `MaxRetries` (default: 3) with exponential backoff.
2. **DLQ Envelope:**
   When an event is moved to the DLQ, it is wrapped in a DLQ envelope containing:
   - `original_event`: Raw message payload.
   - `error_message`: Description of the terminal error.
   - `retry_count`: Number of failed attempts.
   - `failed_at`: Timestamp (UTC) of dead-letter routing.
   - `consumer_group`: Identifier of the failing worker.
3. **Broker Routing:**
   Forwarded to `reservation-events-dlq` topic using Redpanda producer, after which the original offset is committed so subsequent partition messages continue processing.

### C. Workflow Status Normalization

In `internal/workflow/processor.go`:
- Payment approval status transitions reservation from `'held'` to `'confirmed'`.
- Database update sets `status = 'confirmed'`.
- Outbox event emitted is `reservation.confirmed.v1`.
- `isTerminalEvent` accurately recognizes `'confirmed'`, `'cancelled'`, and `'expired'`.

---

## 3. Testing Strategy

1. **Metrics Unit Tests:** Verify HTTP middleware increments counters, measures durations, and returns valid Prometheus exposition format on `/metrics`.
2. **DLQ Unit Tests:**
   - Verify unparseable poison payload is routed to DLQ producer and does not trigger unhandled panics or endless loops.
   - Verify transient retry exhaustion routes to DLQ topic and increments `orderflow_worker_dlq_events_total`.
3. **Live End-to-End Tests:**
   - Probe `/metrics` on running API stack and verify Prometheus format and expected metric keys.
   - Trigger reservation workflow and confirm worker outcome metrics increase.
