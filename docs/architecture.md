# OrderFlow Architecture

## Purpose

OrderFlow is a learning and portfolio project: a ticket reservation backend that demonstrates safe inventory allocation under contention and a recoverable asynchronous workflow. The first deployment is a single cloud VM running Docker Compose; it is not a high-availability production system.

## System shape

```text
Client
  | HTTPS / JSON, JWT
  v
Reverse proxy (deployment)
  v
Reservation API (Go) ------> PostgreSQL (source of truth)
  |                              | reservation + outbox in one transaction
  |                              v
  +----> Redis              Outbox relay
         temporary holds       |
         rate limits            v
                           Redpanda (Kafka API)
                                |
                                v
                       Workflow worker (Go)
                       simulated payment,
                       expiry, compensation
                                |
                                +----> PostgreSQL
                                +----> Redis
```

The reverse proxy is needed only for the public deployment. Local development uses Docker Compose. Redpanda provides a Kafka-compatible event log without requiring a multi-node Kafka installation for the initial project.

## Components and responsibilities

### Reservation API

- Expose documented JSON endpoints to create events, read availability, reserve tickets, complete/cancel a reservation, and read reservation status.
- Authenticate protected operations with JWT; validate payloads, enforce request size limits, and return consistent errors.
- Perform inventory changes in PostgreSQL transactions with row-level locking or an equivalent conditional update. The database enforces that confirmed/reserved quantities cannot exceed capacity.
- Insert the reservation change and corresponding outbox record atomically.

### PostgreSQL

PostgreSQL owns event capacity, reservation state, idempotency records, and the transactional outbox. Redis data is disposable and must be recoverable from PostgreSQL. Use versioned migrations, foreign keys, unique constraints, and indexes supporting actual queries.

### Outbox relay and Redpanda

The relay publishes committed outbox records and marks them delivered only after broker acknowledgement. A crash after publish but before marking may publish the same event again; consumers must safely deduplicate by stable event ID. Events are versioned and carry an ID, type, aggregate/reservation ID, occurrence time, and payload. Poison events must be visible and recoverable; retry/DLQ behavior must be documented and tested before it is called complete.

### Workflow worker

Consume reservation events (and, in checkout mode, `payment.requested.v1`), simulate payment outcomes, update reservation state transactionally, and emit follow-up events through the outbox. Processing is at least once. A processed-event/inbox record and state-transition checks make duplicate delivery harmless. Expired holds trigger release/compensation so capacity becomes available again.

### Redis

Redis is used only for short-lived reservation-hold acceleration and rate limiting. Define TTLs explicitly. On a cache miss, expiration, or Redis outage, the service falls back to PostgreSQL or returns a controlled dependency error; it must never confirm inventory based solely on stale cache data. Document invalidation and recovery behavior.

## Reservation lifecycle

```text
AVAILABLE -> HELD -> CONFIRMED
                 \-> EXPIRED
                 \-> CANCELLED
```

1. The API validates the request and idempotency key.
2. In one PostgreSQL transaction, it locks or conditionally decrements available capacity, creates a `HELD` reservation with an expiry, records the idempotency result, and inserts `ReservationHeld` into the outbox.
3. The outbox relay publishes the event to Redpanda. Duplicate publication is expected and safe.
4. The worker claims the event idempotently and simulates payment. It commits a valid state transition and a corresponding outbox event.
5. Successful payment changes the reservation to `CONFIRMED`. Failure or expiry releases the held capacity and records `CANCELLED` or `EXPIRED`.
6. Clients query reservation status; asynchronous outcomes are explicit rather than inferred from a successful HTTP response.

### Checkout mode (`PAYMENT_MODE=checkout`)

Step 4 changes: the worker acknowledges `ReservationHeld` and leaves the hold open. The customer starts payment with `POST /v1/reservations/{id}/payment`, which records a `payments` row (`processing`) and a `payment.requested.v1` outbox event in one transaction. The worker settles that attempt:

```text
held --pay--> held (payment: processing) --worker--> CONFIRMED   (payment: succeeded)
                                                 \-> held        (payment: declined, retry allowed)
                                                 \-> EXPIRED     (hold lapsed, payment: declined/hold_expired)
```

A decline keeps the hold and its countdown; the customer retries with a new Idempotency-Key. At most one attempt is in flight and one succeeded per reservation (partial unique indexes). Lock order is reservation, then payment, everywhere. See ADR 0007.

The exact endpoint and event schemas are established in API/event contract documents before implementation. State changes must be conditional and reject invalid or repeated transitions safely.

## Reliability and security

- Use bounded timeouts, graceful shutdown, health/readiness endpoints, structured logs, request IDs, Prometheus metrics, and OpenTelemetry distributed tracing with W3C TraceContext propagation.
- Measure API latency/error rate, outbox backlog, consumer lag, retry/dead-letter counts, reservation outcomes, and Redis behavior.
- Distributed traces correlate end-to-end reservation flows from the HTTP API, across the PostgreSQL outbox and Redpanda broker, into the saga worker and terminal transitions in Jaeger.
- Keep secrets in environment/secret configuration, never in source control or logs. Restrict database and broker ports in the public deployment; terminate HTTPS at the reverse proxy.
- Backups and restore steps must be documented for the deployed PostgreSQL data.
- Do not claim exactly-once messaging, multi-region resilience, or high availability.

## Testing and evidence

- Unit tests cover state transitions, idempotency, expiry, and compensation.
- Integration tests use PostgreSQL, Redis, and Redpanda to cover transaction/outbox behavior, duplicate deliveries, worker restarts, and dependency outages.
- A contention test verifies confirmed reservations never exceed capacity. A k6 run reports workload, environment, throughput, p95 latency, error rate, and oversell result. Publish only observed measurements.
- The local demo must be reproducible from documented commands and seeded data.

## Initial non-goals

No real payment integration, separate notification service, Kubernetes, service mesh, multi-region setup, custom broker, or custom consensus/storage engine in the first release. Add extensions only after the core workflow and its failure evidence are complete.
