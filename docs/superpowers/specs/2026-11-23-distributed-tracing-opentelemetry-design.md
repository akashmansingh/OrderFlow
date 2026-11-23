# Distributed Tracing with OpenTelemetry & Jaeger Design Specification

**Feature:** Distributed Tracing with OpenTelemetry, W3C TraceContext Propagation, and Jaeger Visualization  
**Target Release:** Stage 7  
**Author:** OrderFlow Core Engineering  
**Status:** Approved  
**Date:** 2026-11-23  

---

## 1. Problem Statement & Motivation

OrderFlow coordinates high-contention reservations across multiple asynchronous components:
1. Client HTTP request (`POST /v1/events/{id}/reservations`).
2. Two-tier Redis cache query + PostgreSQL row-locking transaction.
3. Transactional Outbox table insertion.
4. Outbox Relay asynchronous polling and publication to Redpanda (Kafka).
5. Workflow Saga Worker consumption, payment simulation, and database state transitions.

Currently, while metrics and logs exist, engineers cannot observe the end-to-end journey of a single reservation request across process and asynchronous messaging boundaries. If a reservation takes 120ms to confirm, it is difficult to determine whether latency was spent waiting in the PostgreSQL lock queue, waiting in the outbox polling interval, waiting in Kafka partition queues, or during payment processing.

Adding **Distributed Tracing with OpenTelemetry** provides complete end-to-end trace correlation:
- Every reservation generates a single unified Trace ID.
- The trace spans client HTTP intake, database transactions, outbox persistence, Kafka message transfer, and saga worker execution.
- Spans visualize exact microsecond latencies for each stage in Jaeger UI.

---

## 2. Architecture & Trace Context Flow

```text
[Client]
   | HTTP POST /v1/events/{id}/reservations
   | (Optional traceparent header)
   v
[HTTP API] -----------------------------------------------------> [Jaeger Collector]
   | Span: "HTTP POST /v1/events/{id}/reservations"                   (OTLP/HTTP :4318)
   | Span: "reservation.CreateReservation"                                  ^
   | Span: "cache.CheckSoldOut"                                             |
   | Span: "db.AcquireEventLock"                                            |
   | Injects traceparent into events.Envelope                               |
   v                                                                        |
[PostgreSQL (outbox table)]                                                 |
   | Envelope JSON contains { ..., "traceparent": "00-4bf92f3577...-01" }   |
   v                                                                        |
[Outbox Relay]                                                              |
   | Extracts traceparent from Envelope                                     |
   | Span: "outbox.PublishEvent"                                            |
   | Injects W3C traceparent into Kafka Record Header                       |
   v                                                                        |
[Redpanda (reservation-events topic)]                                       |
   | Record Header: traceparent = "00-4bf92f3577...-01"                     |
   v                                                                        |
[Worker Consumer]                                                           |
   | Extracts traceparent from Kafka Record Header                          |
   | Span: "workflow.ProcessReservation" (Child/FollowsFrom)                 |
   | Span: "payment.Simulate"                                               |
   | Span: "db.UpdateReservationState"                                      |
   +------------------------------------------------------------------------+
```

---

## 3. Detailed Component Design

### 3.1 Tracing Core Package (`internal/tracing`)

Provide a centralized OpenTelemetry initialization and provider lifecycle manager:
- `Setup(ctx, serviceName, otlpEndpoint string) (shutdown func(context.Context) error, err error)`
- Uses `go.opentelemetry.io/otel/sdk/trace` TracerProvider.
- Uses standard W3C `propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{})`.
- Uses OTLP HTTP/gRPC exporter connecting to Jaeger or OTel Collector (`http://jaeger:4318/v1/traces`).
- Graceful degradation: If `otlpEndpoint` is empty, initializes a no-op tracer with zero performance overhead.

### 3.2 W3C TraceContext Propagation (`events.Envelope` & Kafka Headers)

To safely bridge the database outbox boundary:
- Extend `events.Envelope` struct:
  ```go
  type Envelope struct {
      ID          string          `json:"id"`
      Type        string          `json:"type"`
      AggregateID string          `json:"aggregate_id"`
      OccurredAt  time.Time       `json:"occurred_at"`
      Payload     json.RawMessage `json:"payload"`
      TraceParent string          `json:"traceparent,omitempty"`
      TraceState  string          `json:"tracestate,omitempty"`
  }
  ```
- When creating an event inside `reservation.Service`, inject the active span's trace context into `Envelope.TraceParent`.
- Backward Compatibility: `events.ParseEnvelope` parses envelopes with or without `traceparent`.
- In `internal/messaging/redpanda.go`:
  - When publishing `kgo.Record`, inject `kgo.RecordHeader{Key: "traceparent", Value: []byte(event.TraceParent)}`.

### 3.3 HTTP API Tracing Middleware (`internal/httpapi`)

- Wrap HTTP handlers with OpenTelemetry middleware:
  - Extracts incoming `traceparent` from HTTP headers via W3C propagator.
  - Generates root span if client did not supply one.
  - Sets standard semantic conventions: `http.method`, `http.route`, `http.status_code`, `http.client_ip`.
  - Sets custom domain attributes: `reservation.id`, `event.id`, `user.id`.
  - Injects `traceparent` into HTTP response headers so callers can reference their trace in Jaeger.

### 3.4 Worker Consumer Tracing (`cmd/worker` & `internal/workflow`)

- In `cmd/worker/main.go`:
  - Before calling `processor.Handle(ctx, event)`:
  - Extract `traceparent` from Kafka record header (or fallback to `event.TraceParent`).
  - Start span: `workflow.ProcessReservationEvent`.
  - Tag with `kafka.topic`, `kafka.partition`, `kafka.offset`, `event.id`, `event.type`.
  - Span completes when processing finishes (success, retry, or DLQ).

### 3.5 Infrastructure & Compose Stack

- Add Jaeger All-in-One to `compose.yaml`:
  ```yaml
  jaeger:
    image: jaegertracing/all-in-one:1.62
    environment:
      - COLLECTOR_OTLP_ENABLED=true
    ports:
      - "16686:16686" # Jaeger Web UI
      - "4318:4318"   # OTLP HTTP receiver
    deploy:
      resources:
        limits:
          cpus: '0.5'
          memory: 256M
  ```
- Add environment variable `OTEL_EXPORTER_OTLP_ENDPOINT: http://jaeger:4318` to both `api` and `worker` in `compose.yaml`.

---

## 4. Verification & Testing Strategy

1. **Unit Tests**:
   - `internal/tracing`: Verify provider setup, shutdown, and no-op fallback when endpoint is empty.
   - `internal/events`: Verify `Envelope` serializes and parses `traceparent` correctly.
   - `internal/httpapi`: Verify HTTP middleware creates spans and injects response header.
   - `internal/workflow`: Verify worker starts child span from extracted `traceparent`.
2. **Integration & Live Trace Test**:
   - Create a reservation via API.
   - Query Jaeger API (`GET http://localhost:16686/api/traces?service=orderflow-api`) to assert:
     - Trace exists.
     - Root span is `POST /v1/events/{id}/reservations`.
     - Contains child span `workflow.ProcessReservationEvent` from `orderflow-worker`.
     - End-to-end trace linkage across HTTP $\rightarrow$ DB $\rightarrow$ Kafka $\rightarrow$ Worker is unbroken.
