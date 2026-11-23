# Distributed Tracing with OpenTelemetry & Jaeger Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement end-to-end distributed tracing using OpenTelemetry across the Reservation API, PostgreSQL Outbox, Redpanda Kafka broker, and Saga Worker, with visualization in Jaeger.

**Architecture:** Initialize OpenTelemetry Go SDK in API and Worker with OTLP HTTP exporter. Propagate W3C `traceparent` through HTTP middleware into `events.Envelope` in the outbox table, transfer via Kafka record headers in outbox relay, and resume consumer spans in the saga worker.

**Tech Stack:** Go 1.27, OpenTelemetry Go SDK (`go.opentelemetry.io/otel`), OTLP HTTP Exporter, W3C TraceContext Propagator, Jaeger 1.62 All-in-One, Docker Compose.

**Spec:** [docs/superpowers/specs/2026-11-23-distributed-tracing-opentelemetry-design.md](file:///C:/Work/Projects/OrderFlow/docs/superpowers/specs/2026-11-23-distributed-tracing-opentelemetry-design.md)

---

## Global Constraints

- Never break existing event schema compatibility; `traceparent` in `events.Envelope` must be optional (`omitempty`).
- Zero performance impact when tracing is disabled (no-op tracer provider if OTLP endpoint is not set).
- No hard dependencies on Jaeger being reachable; network errors during trace export must fail-silent without degrading HTTP request handling or reservation transactions.
- Keep commits focused and atomic with descriptive imperative commit messages.

---

## Tasks

### Task 1: Add OpenTelemetry Core Packages and Tracing Manager
**Files:**
- Create: `internal/tracing/tracing.go`
- Test: `internal/tracing/tracing_test.go`
- Modify: `go.mod`, `go.sum`

- [ ] Add OpenTelemetry dependencies (`go.opentelemetry.io/otel`, `go.opentelemetry.io/otel/sdk`, `go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp`).
- [ ] Implement `tracing.Init(ctx context.Context, serviceName, otlpEndpoint string) (func(context.Context) error, error)` in `internal/tracing/tracing.go`.
- [ ] Support no-op tracer provider when `otlpEndpoint == ""`.
- [ ] Write unit tests in `internal/tracing/tracing_test.go` verifying initialization, shutdown, and no-op behavior.
- [ ] Run `go test ./internal/tracing/...` and verify tests pass.

### Task 2: W3C TraceContext Propagation in Events Envelope & Kafka Publisher
**Files:**
- Modify: `internal/events/event.go`
- Modify: `internal/events/event_test.go`
- Modify: `internal/messaging/redpanda.go`
- Modify: `internal/messaging/redpanda_test.go`
- Modify: `internal/reservation/service.go`

- [ ] Add `TraceParent` and `TraceState` fields to `events.Envelope` in `internal/events/event.go`.
- [ ] Verify backward compatibility in `internal/events/event_test.go`.
- [ ] In `internal/reservation/service.go`, inject active span's W3C `traceparent` when constructing `events.Envelope`.
- [ ] In `internal/messaging/redpanda.go`, attach `traceparent` to `kgo.RecordHeader` in `publisher.Publish`.
- [ ] Run `go test ./internal/events/... ./internal/messaging/... ./internal/reservation/...` and verify all pass.

### Task 3: HTTP API Distributed Tracing Middleware
**Files:**
- Create: `internal/httpapi/tracing.go`
- Modify: `internal/httpapi/server.go`
- Test: `internal/httpapi/tracing_test.go`
- Modify: `internal/config/config.go`
- Modify: `cmd/api/main.go`

- [ ] Add `OTEL_EXPORTER_OTLP_ENDPOINT` and `TRACING_ENABLED` to `config.API`.
- [ ] Create HTTP tracing middleware in `internal/httpapi/tracing.go` that extracts incoming `traceparent`, starts root/child span, records HTTP route/status/error attributes, and writes `traceparent` to response header.
- [ ] Wire tracing in `cmd/api/main.go`.
- [ ] Write unit tests in `internal/httpapi/tracing_test.go`.
- [ ] Run `go test ./internal/httpapi/...` and verify all pass.

### Task 4: Worker Consumer Tracing & Saga Trace Continuation
**Files:**
- Modify: `cmd/worker/main.go`
- Modify: `cmd/worker/main_test.go`
- Modify: `internal/workflow/processor.go`
- Modify: `internal/workflow/processor_test.go`
- Modify: `internal/config/config.go`

- [ ] Add `OTEL_EXPORTER_OTLP_ENDPOINT` and `TRACING_ENABLED` to `config.Worker`.
- [ ] In `cmd/worker/main.go`, extract `traceparent` from Kafka record header (or event envelope) and start span `workflow.ProcessReservationEvent`.
- [ ] Wire span attributes in `internal/workflow/processor.go` for payment simulation and terminal state transition.
- [ ] Update worker tests in `cmd/worker/main_test.go` and `internal/workflow/processor_test.go`.
- [ ] Run `go test ./cmd/worker/... ./internal/workflow/...` and verify all pass.

### Task 5: Jaeger Integration in Docker Compose & Live Trace E2E Verification
**Files:**
- Modify: `compose.yaml`
- Modify: `.env.example`
- Create: `tests/e2e/tracing_test.go`
- Create: `docs/decisions/0005-distributed-tracing-opentelemetry.md`

- [ ] Add `jaeger` service (image: `jaegertracing/all-in-one:1.62`) to `compose.yaml` with ports 16686 (UI) and 4318 (OTLP HTTP).
- [ ] Add `OTEL_EXPORTER_OTLP_ENDPOINT: http://jaeger:4318` to `api` and `worker` in `compose.yaml`.
- [ ] Write live trace test `tests/e2e/tracing_test.go` asserting that creating a reservation produces linked spans in Jaeger API across API and Worker.
- [ ] Document architecture decisions in `docs/decisions/0005-distributed-tracing-opentelemetry.md`.
- [ ] Verify live stack, run `gofmt -l .`, static checks, tests, and commit.
