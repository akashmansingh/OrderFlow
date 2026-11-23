# OrderFlow

OrderFlow is a Go ticket-reservation backend built to demonstrate safe inventory allocation, asynchronous workflows, and production-minded operational practices.

```text
Client -> Reservation API -> PostgreSQL transaction + outbox -> Redpanda -> Workflow worker
                                      ^                                  |
                                      +------ confirmed/cancelled/expired +
```

PostgreSQL is the source of truth. The API creates `held` reservations atomically with an outbox record; the worker relays that record to Redpanda and then confirms, cancels, or expires the reservation. Delivery is at least once, so processed-event records and conditional state transitions make duplicate messages safe.

## Live demo & observability

OrderFlow features production-grade observability with Dashboards-as-Code and Prometheus alerting:
- **Public Grafana Monitoring Portal:** [https://grafana.orderflow.example.com](https://grafana.orderflow.example.com) *(Anonymous Viewer enabled)*
- **Public API Endpoint:** [https://api.orderflow.example.com](https://api.orderflow.example.com) *(Protected via Caddy with `/metrics` blocked)*

### Operational Dashboards:

1. **Executive & API Overview:**
   ![Executive Overview](docs/images/grafana-overview.png)
   *Tracks real-time RPS by status, 5xx error ratios, recording rule latency quantiles (p50, p95, p99), saga completion throughput, and multi-window SLO error budget burn rates.*

2. **Asynchronous Pipeline & Sagas:**
   ![Asynchronous Pipeline](docs/images/grafana-pipeline.png)
   *Visualizes transactional outbox backlog count, oldest unpublished row age, Kafka consumer lag per partition, worker retries, and dead-letter queue (DLQ) poison event routing.*

3. **Infrastructure & Resources:**
   ![Infrastructure & Resources](docs/images/grafana-infra.png)
   *Monitors PostgreSQL active connections & transaction commit/rollback rates, Redis ops/sec & sold-out cache hit/miss ratio, Redpanda broker latency, and cAdvisor container CPU/memory.*

### Failure Injection & Self-Healing Demo:

![Worker Failure & Self-Healing](docs/images/failure-demo.gif)
*Simulating saga worker crash under load (`make demo-failure`): outbox backlog accumulates, `StuckSagas` / `OutboxStagnation` alerts fire in Alertmanager, the worker recovers automatically, and PostgreSQL invariants prove 0 inventory oversells.*

---

## Quick start

```powershell
Copy-Item .env.example .env
$jwtSecret = -join ((1..48) | ForEach-Object { '{0:X2}' -f (Get-Random -Maximum 256) })
(Get-Content .env) -replace '^JWT_SECRET=.*', "JWT_SECRET=$jwtSecret" | Set-Content .env
docker compose up -d --build --wait
Invoke-WebRequest http://127.0.0.1:8080/healthz
```

`JWT_SECRET` must be at least 32 bytes and is local to your ignored `.env` file. The API refuses to start without it.

Open pgAdmin at `http://localhost:5050`. The Compose pgAdmin service connects to PostgreSQL at `postgres:5432`; desktop tools on Windows use `localhost:5433`.

## API

| Method | Path | Auth | Purpose |
| --- | --- | --- | --- |
| `GET` | `/healthz` | Public | Process health |
| `GET` | `/readyz` | Public | PostgreSQL readiness |
| `POST` | `/v1/auth/register` | Public | Create a user with email and bcrypt password |
| `POST` | `/v1/auth/login` | Public | Receive a short-lived Bearer access token (rate limited: 5 req/min) |
| `POST` | `/v1/events` | Bearer | Create a ticketed event |
| `GET` | `/v1/events/{eventID}` | Bearer | Read capacity and availability state |
| `POST` | `/v1/events/{eventID}/reservations` | Bearer | Create a held reservation; requires `Idempotency-Key` (rate limited: 10 req/min) |
| `GET` | `/v1/reservations/{reservationID}` | Bearer | Read your asynchronous reservation status |

Register with `{"email":"user@example.com","password":"correct horse battery"}`, log in with the same credentials, then send `Authorization: Bearer <access_token>` on protected routes. The access token subject becomes the reservation `customer_id`; a client-supplied `customer_id` is rejected. Redis enforces sliding-window rate limits and fast-fails sold-out inventory checks, failing open if Redis is unavailable.

## Services

| Service | Local address | Role |
| --- | --- | --- |
| API | `http://localhost:8080` | Reservation HTTP API |
| PostgreSQL | `localhost:5433` | Authoritative data and outbox |
| pgAdmin | `http://localhost:5050` | Database administration |
| Redis | `localhost:6379` | Fast-path sold-out cache and rate-limiting |
| Redpanda | `localhost:19092` | Kafka-compatible event broker |
| Worker | Compose-only | Outbox relay, consumer, payment simulation, expiry |

## Development and operations

- [Architecture](docs/architecture.md)
- [Local Docker and pgAdmin setup](docs/local-development.md)
- [Testing](docs/testing.md)
- [JWT authentication ADR](docs/decisions/0001-jwt-authentication.md)
- [Redis caching and rate limiting ADR](docs/decisions/0002-redis-caching-and-rate-limiting.md)
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

Payments are simulated with `SIMULATED_PAYMENT_RESULT=approve|reject`. JWT access tokens provide first-party authentication with no refresh tokens, revocation list, roles, or external identity provider. Redis caching/rate limiting, dead-letter handling, tracing, real payments, Kubernetes, and high availability are planned later; this project does not claim exactly-once messaging.
