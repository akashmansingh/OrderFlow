# Outbox Workflow Worker Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Publish committed reservation events to Redpanda and process held reservations asynchronously into confirmed, cancelled, or expired states.

**Architecture:** A single Go worker process owns an outbox relay loop and a workflow-consumer/expiry loop. PostgreSQL commits all inventory and state changes atomically; Redpanda delivery and offset commits are at-least-once, so stable event IDs and `processed_events` make re-delivery safe.

**Tech Stack:** Go 1.27, `pgx/v5`, `franz-go/kgo`, PostgreSQL 17, Redpanda Kafka API, Docker Compose, GitHub Actions.

**Spec:** `docs/superpowers/specs/2026-11-17-outbox-workflow-worker-design.md`

## Global Constraints

- PostgreSQL remains authoritative for reservation and inventory state.
- Publish to `reservation-events`; consume as `orderflow-workflow-worker`.
- Use at-least-once delivery; do not claim exactly-once behavior.
- `SIMULATED_PAYMENT_RESULT` defaults to `approve` and supports `reject` for deterministic failure tests.
- Terminal events are acknowledged and ignored by the worker; stale held events are logged and acknowledged.
- E2E tests stay opt-in and are not run unless `ORDERFLOW_E2E_BASE_URL` is explicitly supplied.
- Commit every changed file separately with an imperative, scoped message, per repository direction.

## Review Focus

- Relay crash between successful broker acknowledgement and `published_at` update must produce a duplicate-safe event, tested in Task 2.
- Duplicate `reservation.held.v1` messages must alter inventory once, tested in Task 3.
- A held reservation received after `expires_at` must release inventory and become `expired`, tested in Task 3.
- Rejected payment must release held inventory and emit a cancellation event, tested in Task 3.
- A worker must never commit a Kafka offset before its PostgreSQL transaction succeeds, tested in Task 4.

---

### Task 1: Worker configuration and event contract

**Files:**
- Modify: `go.mod`, `go.sum`
- Modify: `internal/config/config.go`
- Create: `internal/events/event.go`
- Create: `internal/events/event_test.go`

**Interfaces:**
- Produces `events.Envelope` with `ID`, `Type`, `AggregateID`, `OccurredAt`, and raw `Payload`.
- Produces `config.Config` fields `KafkaBrokers []string`, `KafkaTopic string`, `KafkaGroupID string`, `WorkerPollInterval time.Duration`, and `SimulatedPaymentResult string`.

- [ ] Write `internal/events/event_test.go` first for valid JSON envelope decoding and rejection of missing IDs/types.
- [ ] Run `go test ./internal/events -run TestEnvelope -v` and confirm failure because the package does not exist.
- [ ] Add `events.Envelope` parsing/validation and configuration parsing with default topic, group, polling interval, and approve result.
- [ ] Run focused tests, then `go test ./...` with the E2E suite skipped because no opt-in URL is set.
- [ ] Commit `internal/events/event.go`, `internal/events/event_test.go`, `internal/config/config.go`, `go.mod`, and `go.sum` as separate logical file commits.

### Task 2: PostgreSQL outbox repository and relay

**Files:**
- Create: `internal/outbox/repository.go`
- Create: `internal/outbox/relay.go`
- Create: `internal/outbox/relay_test.go`
- Create: `internal/messaging/redpanda.go`

**Interfaces:**
- Consumes `events.Envelope` and `pgxpool.Pool`.
- Produces `outbox.Relay.Run(context.Context) error` and `outbox.Publisher.Publish(context.Context, events.Envelope) error`.
- Produces `messaging.NewProducer(brokers []string) (*kgo.Client, error)`.

- [ ] Write a failing relay test showing an event remains unpublished after a publish failure and is marked published only after `Publisher.Publish` succeeds.
- [ ] Run the focused relay test and confirm it fails because the relay/repository are absent.
- [ ] Implement ordered unpublished-event claiming with `FOR UPDATE SKIP LOCKED`, publish attempt recording, stable event ID key/header, and `published_at` update after acknowledgement.
- [ ] Run focused tests and `go test ./...`; do not run opt-in E2E tests.
- [ ] Commit each created or modified file separately after its check passes.

### Task 3: Reservation workflow and expiry behavior

**Files:**
- Create: `internal/workflow/processor.go`
- Create: `internal/workflow/processor_test.go`
- Create: `internal/workflow/expiry.go`
- Create: `internal/workflow/expiry_test.go`
- Modify: `internal/reservation/service.go`

**Interfaces:**
- Consumes `events.Envelope`, `pgxpool.Pool`, and `SimulatedPaymentResult`.
- Produces `workflow.Processor.Handle(context.Context, events.Envelope) error` and `workflow.Expiry.RunOnce(context.Context) error`.
- Emits terminal payloads with `reservation_id`, `event_id`, `customer_id`, `quantity`, `status`, and `reason`.

- [ ] Write failing processor tests for approved hold, rejected hold, duplicate held event, expired held event, terminal-event acknowledgement, and stale reservation acknowledgement.
- [ ] Run focused processor tests and confirm failure because the workflow package is absent.
- [ ] Implement conditional inventory transitions, `processed_events` insert-first idempotency, terminal outbox records, simulated payment decision, and stale/terminal acknowledgement handling.
- [ ] Write a failing expiry test for releasing an expired held reservation once.
- [ ] Implement `Expiry.RunOnce` with locked batches, transition/outbox writes, then run focused and full Go tests.
- [ ] Commit each file separately after the relevant focused test passes.

### Task 4: Worker process, Compose integration, and integration verification

**Files:**
- Create: `cmd/worker/main.go`
- Create: `cmd/worker/main_test.go`
- Modify: `compose.yaml`
- Modify: `.env.example`
- Modify: `docs/local-development.md`

**Interfaces:**
- Consumes `config.Config`, `outbox.Relay`, `workflow.Processor`, `workflow.Expiry`, and Redpanda’s consumer client.
- Produces a graceful worker process that starts relay, consumer, and expiry loops and shuts them down on `SIGTERM`.

- [ ] Write a failing worker lifecycle test that requires consumer offset acknowledgement after successful handling only.
- [ ] Run it and confirm failure because `cmd/worker` is absent.
- [ ] Implement the worker process with manual Kafka offset commit, bounded shutdown, and structured logging.
- [ ] Add Compose worker wiring, configuration defaults, and local instructions for inspecting worker logs and the `reservation-events` topic.
- [ ] Run `go test ./...`, `go build ./cmd/api`, `go build ./cmd/worker`, and `docker compose config --quiet`; leave opt-in E2E execution to an explicit user request.
- [ ] Commit each changed file separately after verification.

### Task 5: README as the project entry point

**Files:**
- Modify: `README.md`

**Interfaces:**
- Documents the API at `http://localhost:8080`, pgAdmin at `http://localhost:5050`, PostgreSQL host mapping `localhost:5433`, `reservation-events`, worker lifecycle, links to all operational docs, and known limitations.

- [ ] Rewrite the README with purpose, architecture diagram, quick start, endpoint examples, service/port table, event lifecycle, test guidance, CI/CD summary, repository layout, and current non-goals.
- [ ] Check all linked local documents and commands for accuracy without running opt-in E2E tests.
- [ ] Commit `README.md` separately.

## Self-review

- Tasks 1–4 implement every relay, consumer, expiry, event, and failure guarantee in the spec.
- Task 5 covers the requested README rewrite and links it to the existing operational documents.
- Review-focus scenarios are assigned to Tasks 2–4.
- JWT, Redis behavior, DLQ, tracing, real payments, and Kubernetes remain explicitly outside this increment.
