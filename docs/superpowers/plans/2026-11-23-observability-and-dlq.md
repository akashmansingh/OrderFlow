# Stage 5: Observability, Prometheus Metrics & Dead-Letter Queue Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement Prometheus metrics instrumentation (`/metrics`) across API and background worker, poison message Dead-Letter Queue (DLQ) routing on Redpanda, and workflow status normalization.

**Architecture:** Create an `internal/metrics` package registering typed Prometheus metrics using an injectable registry. Wrap HTTP routes in metrics middleware and expose `GET /metrics`. Instrument Outbox relay backlog and worker outcome counters. Wrap worker message handling with poison detection and retry-exhaustion logic that forwards dead letters to `reservation-events-dlq`.

**Tech Stack:** Go 1.27, `github.com/prometheus/client_golang/prometheus`, `github.com/prometheus/client_golang/prometheus/promhttp`, PostgreSQL 17, Redpanda (Kafka), Redis 7.4.

**Spec:** `docs/superpowers/specs/2026-11-23-observability-and-dlq-design.md`

## Global Constraints
- PostgreSQL remains the sole source of truth for inventory.
- Maintain at-least-once processing semantics; do not claim exactly-once.
- Never crash consumer loops on poison messages; route to DLQ topic and commit offset.
- All unit and integration tests must run without external network access or shared global registry collisions.

## Review Focus
- Handling malformed JSON or invalid schema in worker: must route to DLQ and not crash or loop indefinitely.
- Prometheus registry collision in parallel tests: must use isolated `prometheus.NewRegistry()` per test or instance.
- Transient error retry exhaustion: must retry up to configured limit before forwarding to DLQ.
- Correct status normalization: approved payments must transition reservations to `'confirmed'`, not `'approve'`.
- `/metrics` endpoint accessibility: must return 200 OK with Prometheus exposition format without requiring auth.

---

### Task 1: Centralized Metrics Subsystem (`internal/metrics`)

**Files:**
- Create: `internal/metrics/metrics.go`
- Test: `internal/metrics/metrics_test.go`

- [x] Write failing unit test in `internal/metrics/metrics_test.go` checking counter increments, histogram observations, and gauge sets on an isolated registry.
- [x] Implement `internal/metrics/metrics.go` defining `Metrics` struct with HTTP, Redis, Outbox, and Worker metrics registered to an injectable `prometheus.Registerer`.
- [x] Run `go test -v ./internal/metrics/...` and confirm test passes.
- [x] Commit with message: `Add centralized Prometheus metrics subsystem`.

---

### Task 2: HTTP Metrics Middleware and `/metrics` Endpoint (`internal/httpapi`)

**Files:**
- Modify: `internal/httpapi/server.go`
- Modify: `internal/httpapi/ratelimit.go`
- Test: `internal/httpapi/metrics_test.go`

- [x] Write failing test in `internal/httpapi/metrics_test.go` verifying that `GET /metrics` returns 200 OK and reflects request counts, durations, and rate-limit rejections.
- [x] Integrate metrics middleware in `internal/httpapi/server.go` recording `orderflow_http_requests_total` and `orderflow_http_request_duration_seconds`.
- [x] Expose `GET /metrics` route with `promhttp.HandlerFor`.
- [x] Instrument rate limiting in `internal/httpapi/ratelimit.go` to increment `orderflow_ratelimit_rejected_total` on 429 status.
- [x] Run `go test -v ./internal/httpapi/...` and confirm all tests pass.
- [x] Commit with message: `Instrument HTTP server with Prometheus metrics and /metrics endpoint`.

---

### Task 3: Workflow Payment Status Normalization (`internal/workflow`)

**Files:**
- Modify: `internal/workflow/processor.go`
- Modify: `internal/workflow/processor_test.go`

- [x] Write failing test in `internal/workflow/processor_test.go` checking that payment approval sets reservation status to `"confirmed"` and emits `reservation.confirmed.v1`.
- [x] Update `internal/workflow/processor.go` lines 95-135 replacing `"approve"` with `"confirmed"`.
- [x] Run `go test -v ./internal/workflow/...` and confirm all tests pass.
- [x] Commit with message: `Normalize simulated payment approval status to confirmed`.

---

### Task 4: Outbox & Worker Metrics Instrumentation

**Files:**
- Modify: `internal/outbox/relay.go`
- Modify: `internal/workflow/processor.go`
- Modify: `internal/workflow/expiry.go`
- Modify: `internal/cache/redis.go`
- Test: `internal/outbox/relay_test.go`

- [x] Update `internal/outbox/relay.go` to gauge `orderflow_outbox_backlog_records` during publishing sweeps.
- [x] Update `internal/workflow/processor.go` and `expiry.go` to record `orderflow_worker_events_processed_total` with appropriate outcome labels (`confirmed`, `cancelled`, `expired`).
- [x] Update `internal/cache/redis.go` to record cache hits, misses, and fail-open occurrences.
- [x] Run `go test -v ./internal/outbox/... ./internal/workflow/... ./internal/cache/...` and confirm passing.
- [x] Commit with message: `Instrument outbox relay, cache, and worker outcomes with Prometheus metrics`.

---

### Task 5: Dead-Letter Queue (DLQ) & Poison Message Handling (`internal/workflow` & `cmd/worker`)

**Files:**
- Create: `internal/workflow/dlq.go`
- Modify: `internal/workflow/processor.go`
- Modify: `cmd/worker/main.go`
- Test: `internal/workflow/dlq_test.go`

- [x] Write failing tests in `internal/workflow/dlq_test.go` verifying that malformed payloads and exhausted retry errors route to the DLQ publisher and increment `orderflow_worker_dlq_events_total`.
- [x] Implement `DLQEnvelope` and `DLQHandler` in `internal/workflow/dlq.go`.
- [x] Wire DLQ publisher in `cmd/worker/main.go` targeting `reservation-events-dlq` topic.
- [x] Run `go test -v ./internal/workflow/... ./cmd/worker/...` and verify all tests pass.
- [x] Commit with message: `Add dead-letter queue routing for poison and failing workflow messages`.

---

### Task 6: Live End-to-End Verification of Metrics and DLQ

**Files:**
- Modify: `tests/e2e/api_test.go`

- [x] Add live E2E test `TestPrometheusMetricsEndpoint` verifying `/metrics` returns standard Prometheus metrics with expected keys (`orderflow_http_requests_total`, `orderflow_ratelimit_rejected_total`, etc.).
- [x] Execute full test suite `go test -v ./cmd/... ./internal/...` and live E2E suite `go test -v -count=1 ./tests/e2e`.
- [x] Commit with message: `Add live E2E tests for Prometheus metrics scraping`.
