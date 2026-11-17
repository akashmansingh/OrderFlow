# OrderFlow

OrderFlow is a Go ticket-reservation backend built to demonstrate safe inventory allocation, asynchronous workflows, and production-minded operational practices.

```text
Client -> Reservation API -> PostgreSQL transaction + outbox -> Redpanda -> Workflow worker
                                      ^                                  |
                                      +------ confirmed/cancelled/expired +
```

PostgreSQL is the source of truth. The API creates `held` reservations atomically with an outbox record; the worker relays that record to Redpanda and then confirms, cancels, or expires the reservation. Delivery is at least once, so processed-event records and conditional state transitions make duplicate messages safe.

## Quick start

```powershell
Copy-Item .env.example .env
docker compose up -d --build --wait
Invoke-WebRequest http://127.0.0.1:8080/healthz
```

Open pgAdmin at `http://localhost:5050`. The Compose pgAdmin service connects to PostgreSQL at `postgres:5432`; desktop tools on Windows use `localhost:5433`.

## API

| Method | Path | Purpose |
| --- | --- | --- |
| `GET` | `/healthz` | Process health |
| `GET` | `/readyz` | PostgreSQL readiness |
| `POST` | `/v1/events` | Create a ticketed event |
| `GET` | `/v1/events/{eventID}` | Read capacity and availability state |
| `POST` | `/v1/events/{eventID}/reservations` | Create a held reservation; requires `Idempotency-Key` |
| `GET` | `/v1/reservations/{reservationID}` | Read asynchronous reservation status |

## Services

| Service | Local address | Role |
| --- | --- | --- |
| API | `http://localhost:8080` | Reservation HTTP API |
| PostgreSQL | `localhost:5433` | Authoritative data and outbox |
| pgAdmin | `http://localhost:5050` | Database administration |
| Redis | `localhost:6379` | Future holds/cache and rate-limit support |
| Redpanda | `localhost:19092` | Kafka-compatible event broker |
| Worker | Compose-only | Outbox relay, consumer, payment simulation, expiry |

## Development and operations

- [Architecture](docs/architecture.md)
- [Local Docker and pgAdmin setup](docs/local-development.md)
- [Testing](docs/testing.md)
- [CI/CD and VM deployment](docs/deployment.md)
- [Repository instructions](AGENTS.md)

Run internal checks without invoking the opt-in live E2E suite:

```powershell
go test ./cmd/... ./internal/...
go build ./cmd/api
go build ./cmd/worker
docker compose config --quiet
```

## Current boundaries

Payments are simulated with `SIMULATED_PAYMENT_RESULT=approve|reject`. JWT authentication, Redis caching/rate limiting, dead-letter handling, tracing, real payments, Kubernetes, and high availability are planned later; this project does not claim exactly-once messaging.
