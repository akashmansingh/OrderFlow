# ADR 0005: Distributed Tracing with OpenTelemetry and Jaeger

## Status

Accepted

## Context

OrderFlow processes ticket reservations through an asynchronous, distributed event pipeline spanning the HTTP API, transactional PostgreSQL database outbox, Redpanda (Kafka-compatible broker), and asynchronous saga background worker.

While Prometheus metrics (ADR 0003) provide aggregated system telemetry (rates, latencies, failure counters), diagnosing bottlenecks, tracking individual reservation request journeys, and pinpointing cross-boundary delays across the database outbox and broker requires distributed tracing.

## Decisions

1. **Standard & SDK:**
   - Adopt standard OpenTelemetry Go SDK (`go.opentelemetry.io/otel`, `go.opentelemetry.io/otel/trace`, `go.opentelemetry.io/otel/sdk`).
   - Use standard W3C TraceContext propagation format (`traceparent: 00-{trace_id}-{span_id}-{trace_flags}`).

2. **Cross-Boundary Trace Propagation:**
   - **HTTP Boundary:** `internal/httpapi/tracing.go` extracts incoming `traceparent` headers, starts server spans (`http.METHOD /path`), and injects `traceparent` into response headers so callers can correlate their requests with backend traces.
   - **Transactional Database Outbox Boundary:** When creating reservations, the active span context is captured into the outbox event payload (`events.Envelope.TraceParent` / `traceparent` JSON field). This bridges the database outbox without requiring any breaking SQL schema migrations.
   - **Kafka / Redpanda Broker Boundary:** `internal/messaging/redpanda.go` copies `traceparent` and `tracestate` into Kafka record headers (`kgo.RecordHeader`) when publishing.
   - **Worker Consumer & Saga Boundary:** `cmd/worker/main.go` and `internal/workflow/processor.go` extract trace context from record headers (falling back to the event envelope payload), starting consumer spans (`workflow.ProcessReservationEvent`) and child saga spans (`workflow.ProcessSaga`), linking terminal outbox events to the original trace.

3. **Backend & Collector:**
   - Use Jaeger All-in-One (`jaegertracing/all-in-one:latest`) accepting OTLP/HTTP over port 4318 and exposing the Jaeger search UI on port 16686.
   - Resource limits (0.5 CPU, 256MB RAM) configured in `compose.yaml`.

4. **Zero-Overhead / Fail-Open Operation:**
   - When `OTEL_EXPORTER_OTLP_ENDPOINT` is unset or `ORDERFLOW_TRACING_ENABLED=false`, the system falls back to an OpenTelemetry `noop.NewTracerProvider()`, adding zero network overhead.
   - If Jaeger is down or unreachable, OTLP exporters drop trace batches silently without blocking HTTP handlers or failing background processing.

## Consequences

- Full end-to-end trace visibility from client HTTP call, through PostgreSQL transactional outbox, through Redpanda Kafka broker, to worker payment processing and state transition.
- Immediate debugging capability in Jaeger UI (`http://localhost:16686`) for slow requests, concurrency bottlenecks, and failed sagas.
- Zero breaking changes to existing database schemas or Kafka consumers.
- No impact on system availability if the tracing backend experiences an outage.
