# OrderFlow

OrderFlow is a high-contention ticket reservation backend and box-office web application designed to demonstrate zero-drift inventory allocation, asynchronous saga orchestration, and production-grade observability. Built in Go, PostgreSQL, Redis, Redpanda, and React 19, OrderFlow guarantees that tickets are never oversold under extreme concurrent demand while providing transparent, real-time operational evidence across the entire distributed system.

---

## Overview

OrderFlow is designed to:

- **Prevent Overselling & Inventory Drift:** Ensure ticket capacity invariants are strictly upheld during flash sales using PostgreSQL ACID transactions with row-level locking (`SELECT ... FOR UPDATE`).
- **Eliminate Dual-Write Inconsistencies:** Use the **Transactional Outbox Pattern** to write reservation state and event payloads atomically within the same database transaction.
- **Orchestrate Asynchronous Sagas:** Coordinate multi-step reservation workflows (`Held` &rarr; `Paid` &rarr; `Confirmed` / `Cancelled` / `Expired`) over Redpanda with at-least-once event delivery, idempotent consumer deduplication, and Dead-Letter Queue (DLQ) poison-message routing.
- **Accelerate Critical Paths with Safe Fallbacks:** Protect database performance using Redis sliding-window rate limiters and sold-out inventory fast-paths that automatically fail open if the cache becomes unavailable.
- **Provide End-to-End Distributed Tracing:** Propagate W3C `traceparent` headers across HTTP requests, Kafka event payloads, database transactions, and background worker jobs using OpenTelemetry and Jaeger.
- **Deliver an Intuitive, Responsive Web Interface:** Provide a box-office web UI featuring realistic concert tickets, live hold countdowns, trace inspection, and fluid responsive design across mobile, tablet, and desktop viewports.

---

## Features

- **Interactive Box-Office Web UI:**
  - *Ticket Motif & Animations:* Authentic perforated concert ticket styling, live availability punch strips, and stamp-slam status animations.
  - *Responsive Across All Devices:* Adaptive ticket architecture that transitions between horizontal stubs on desktop/tablet and bottom-perforated stubs with side notch masking on mobile (&le; 576px).
  - *Touch-Optimized Controls:* 5-column touch grid for mobile ticket quantity selection and thumb-friendly action buttons.
  - *Trace Inspector Drawer:* Slide-out drawer displaying recent API calls, latency, request IDs, idempotency keys, and direct deep-links to Jaeger traces.
  - *Decoupled Authentication:* Clean modal authentication with registration confirmation alerts and input isolation.

- **High-Contention Inventory Engine:**
  - *Row-Level Locking:* Atomically holds available seats with pessimistic locks, eliminating race conditions and double-booking.
  - *Authoritative Consistency:* PostgreSQL remains the sole authority for confirmed inventory and reservation status; confirmed + held quantity never exceeds total capacity.
  - *Sold-Out Fast Path:* Redis accelerates sold-out checks to quickly reject requests when capacity is exhausted without querying the database.

- **Asynchronous Saga & Event Pipeline:**
  - *Transactional Outbox:* Decouples API response latency from event publishing while guaranteeing reliable event dispatch.
  - *Redpanda Event Streaming:* High-throughput, Kafka-compatible event streaming for `reservation.held.v1`, `reservation.confirmed.v1`, and `reservation.cancelled.v1`.
  - *Background Workflow Worker:* Consumes outbox events, coordinates simulated payment settlement, releases holds upon expiration (10-minute hold window), and persists final saga states.
  - *Dead-Letter Queue (DLQ):* Automatically routes malformed payloads or unrecoverable poison events to a dedicated DLQ topic with associated Prometheus metrics.

- **Production Observability & Monitoring:**
  - *Dashboards-as-Code:* Pre-provisioned Grafana dashboards for Executive Overview, Asynchronous Pipeline/Sagas, and System Infrastructure.
  - *Prometheus Alerting:* 6 recording rules (computing p50/p95/p99 latencies, error ratios, and SLO burn rates) and 12 alert rules (`StuckSagas`, `DeadLetterQueueActive`, `OutboxStagnation`, `TargetDown`, etc.).
  - *Distributed Tracing:* Complete trace context propagation through HTTP headers and Kafka headers, rendered in Jaeger UI.

- **Security & Rate Limiting:**
  - *Sliding-Window Limiter:* Redis-backed leaky bucket protecting login endpoints (5 req/min) and reservation holds (10 req/min).
  - *JWT Bearer Authentication:* Stateless HMAC-SHA256 JWT tokens with bcrypt-hashed passwords (minimum 12 characters).

---

## Architecture

OrderFlow pairs a high-performance HTTP API with a decoupled asynchronous worker using PostgreSQL as the authoritative source of truth and Redpanda as the event backbone.

```mermaid
flowchart TD
    subgraph Client ["Client Layer"]
        Web["Web Application (React 19 / Vite)"]
        Curl["API Clients / Load Testers"]
    end

    subgraph Ingress ["Edge & Gateway"]
        Caddy["Caddy Reverse Proxy (:80 / :443)"]
    end

    subgraph CoreServices ["Application Services"]
        API["OrderFlow API (:8080)"]
        Worker["OrderFlow Workflow Worker"]
    end

    subgraph Persistence ["Authoritative State & Cache"]
        Postgres[("PostgreSQL 16\n• reservations\n• events\n• outbox_events\n• processed_events")]
        Redis[("Redis 7\n• Sliding-window rate limit\n• Sold-out fast path")]
    end

    subgraph Messaging ["Event Broker"]
        Redpanda{{"Redpanda (Kafka API)\n• reservation-events\n• reservation-dlq"}}
    end

    subgraph Observability ["Telemetry & Monitoring"]
        Prometheus["Prometheus (:9090)"]
        Grafana["Grafana Dashboards (:3000)"]
        Jaeger["Jaeger Tracing (:16686)"]
        Alertmanager["Alertmanager (:9093)"]
    end

    Web -->|HTTP / REST| Caddy
    Curl -->|HTTP / REST| Caddy
    Caddy -->|/api/*| API

    API -->|Rate Limit & Cache Check| Redis
    API -->|ACID Transaction\nHold Reservation + Outbox| Postgres

    Worker -->|Poll Unpublished Outbox| Postgres
    Worker -->|Produce Events| Redpanda
    Redpanda -->|Consume Events| Worker
    Worker -->|Simulate Payment &\nFinalize Saga Status| Postgres

    API -.->|OTel Traces| Jaeger
    Worker -.->|OTel Traces| Jaeger
    API -.->|Expose Metrics| Prometheus
    Worker -.->|Expose Metrics| Prometheus
    Prometheus -->|Alerts| Alertmanager
    Prometheus -->|Metrics Visuals| Grafana
```

---

## Installation

### Prerequisites

Ensure you have the following installed on your system:
- **Docker & Docker Compose** (Docker Desktop or Linux Docker Engine v24+)
- **Go** (v1.22+ if compiling or running tests locally)
- **Node.js** (v20+ and npm for running the frontend locally)
- **Git**

### 1. Clone the Repository

```bash
git clone https://github.com/theRiddler9/OrderFlow.git
cd OrderFlow
```

### 2. Configure Environment

Copy the example environment configuration and generate a secure 32-byte secret for JWT signing:

**On Linux / macOS / WSL (Bash):**
```bash
cp .env.example .env
jwt_secret=$(openssl rand -hex 32)
sed -i "s/^JWT_SECRET=.*/JWT_SECRET=${jwt_secret}/" .env
```

**On Windows (PowerShell):**
```powershell
Copy-Item .env.example .env
$jwtSecret = -join ((1..48) | ForEach-Object { '{0:X2}' -f (Get-Random -Maximum 256) })
(Get-Content .env) -replace '^JWT_SECRET=.*', "JWT_SECRET=$jwtSecret" | Set-Content .env
```

### 3. Start the Backend & Infrastructure

Launch all infrastructure containers (PostgreSQL, Redis, Redpanda, Jaeger, Prometheus, Grafana, API, and Worker):

```bash
docker compose up -d --build --wait
```

Verify that the core API is healthy:
```bash
curl http://localhost:8080/healthz
# Response: {"status":"healthy"}
```

### 4. Seed Showcase Demo Events

Seed the database with realistic concert and festival events (*Coldplay Live 2026*, *Taylor Swift | The Eras Tour*, *Midnight Jazz Cellar*, and *Lakeside Film Night*):

**On Linux / macOS / WSL:**
```bash
./scripts/seed_demo.sh
```

**On Windows:**
```powershell
.\scripts\seed_demo.ps1
```

### 5. Launch the Web Frontend

Navigate to the `web/` directory, install dependencies, and start the Vite development server:

```bash
cd web
npm install
npm run dev
```

Open your browser at **`http://localhost:5173`**.

---

## Configuration

Core configuration settings are managed via environment variables defined in `.env`:

```env
# Application Ports & Environment
PORT=8080
ENVIRONMENT=development
LOG_LEVEL=info

# PostgreSQL
POSTGRES_HOST=postgres
POSTGRES_PORT=5432
POSTGRES_DB=orderflow
POSTGRES_USER=orderflow
POSTGRES_PASSWORD=orderflow
DATABASE_URL=postgres://orderflow:orderflow@postgres:5432/orderflow?sslmode=disable

# Redis (Caching & Rate Limiting)
REDIS_ADDRESS=redis:6379
REDIS_TIMEOUT=1
RATE_LIMIT_LOGIN_PER_MINUTE=5
RATE_LIMIT_RESERVATION_PER_MINUTE=10
ORDERFLOW_RATE_LIMIT_ENABLED=true

# Redpanda / Kafka
KAFKA_BROKERS=redpanda:9092
KAFKA_TOPIC=reservation-events
KAFKA_GROUP_ID=orderflow-workflow-worker
WORKER_POLL_SECONDS=1

# Saga Payment Simulation (approve | reject)
SIMULATED_PAYMENT_RESULT=approve

# OpenTelemetry & Distributed Tracing
OTEL_EXPORTER_OTLP_ENDPOINT=http://jaeger:4318
ORDERFLOW_TRACING_ENABLED=true
```

---

## Usage

### 1. Web Application

Access the application at **`http://localhost:5173`**:
1. **Create an Account:** Click **Sign in**, toggle to **Create account**, and enter your credentials.
2. **Sign In:** Use your newly created account to log in.
3. **Choose Tickets:** Select any event (*Coldplay*, *Taylor Swift*, etc.) and pick your ticket quantity (1&ndash;10).
4. **Follow the Saga:** Click **Reserve tickets**. Your reservation will appear in **Your holds** and step live through `Held` &rarr; `Paid` &rarr; `Confirmed`.
5. **Inspect Traces:** Click **Inspect trace** on any hold card or in the header to view W3C traceparents and launch the full distributed trace in Jaeger.

### 2. Service & Monitoring Endpoints

| Service | Local Address | Credentials / Role |
| :--- | :--- | :--- |
| **Web UI** | `http://localhost:5173` | Ticket booking interface |
| **Reservation API** | `http://localhost:8080` | Core REST API |
| **Grafana Dashboards** | `http://localhost:3000` | `admin` / `orderflow` (or anonymous viewing) |
| **Jaeger Tracing** | `http://localhost:16686` | OpenTelemetry distributed traces |
| **Prometheus** | `http://localhost:9090` | Raw metrics & PromQL queries |
| **Alertmanager** | `http://localhost:9093` | Alert routing and status |
| **pgAdmin** | `http://localhost:5050` | `admin@orderflow.internal` / `orderflow` |

### 3. API Endpoints

| Method | Path | Auth | Purpose |
| :--- | :--- | :--- | :--- |
| `GET` | `/healthz` | Public | Process health probe |
| `GET` | `/readyz` | Public | Database readiness probe |
| `POST` | `/v1/auth/register` | Public | Create account with email & password |
| `POST` | `/v1/auth/login` | Public | Authenticate and obtain JWT Bearer token |
| `GET` | `/v1/events` | Bearer | List on-sale events with real-time remaining capacity |
| `POST` | `/v1/events` | Bearer | Create a new ticketed event with specified capacity |
| `GET` | `/v1/events/{id}` | Bearer | Retrieve event details and availability |
| `POST` | `/v1/events/{id}/reservations` | Bearer | Hold tickets atomically (Requires `Idempotency-Key` header) |
| `GET` | `/v1/reservations/{id}` | Bearer | Check asynchronous reservation settlement status |
| `GET` | `/metrics` | Public | Prometheus scraping endpoint |

---

## Technical Details

### Zero-Overselling Concurrency Invariant

To guarantee that inventory is never oversold under race conditions, the reservation transaction locks the targeted event record using pessimistic row-level locking:

```sql
SELECT capacity,
       COALESCE((SELECT SUM(quantity) FROM reservations WHERE event_id = $1 AND status = 'confirmed'), 0) AS confirmed_qty,
       COALESCE((SELECT SUM(quantity) FROM reservations WHERE event_id = $1 AND status = 'held' AND expires_at > NOW()), 0) AS held_qty
FROM events
WHERE id = $1
FOR UPDATE;
```

If `confirmed_qty + held_qty + requested_qty > capacity`, the transaction immediately aborts with an HTTP `409 Conflict`, guaranteeing zero overselling regardless of how many concurrent workers or users attempt to checkout simultaneously.

### Transactional Outbox Pattern

To prevent dual-write failure modes (e.g. reserving a ticket in PostgreSQL but failing to publish to Kafka due to network partitioning), reservation creation writes to both the `reservations` table and an `outbox_events` table within the same database transaction. The background worker continuously polls for unpublished records, emits them to Redpanda, and marks them published only after receiving broker acknowledgements.

### Distributed Tracing with W3C Trace Context

Every request entering the system carries or generates a W3C trace context (`traceparent`). The context is propagated through:
1. HTTP request headers to database queries (`db.system=postgresql`).
2. Kafka record headers when the outbox event is emitted.
3. Worker processing spans when the saga consumes the message and simulates payment.
4. Response headers (`Access-Control-Expose-Headers: traceparent`) back to the frontend, allowing instantaneous inspection in Jaeger.

---

## Testing & Verification

Run internal unit, cache, rate limit, and saga tests:

```bash
# Run Go backend test suite
go test ./internal/...

# Run frontend typecheck and production build
cd web
npm run build
```

Run Docker Compose configuration validation and Prometheus alert rule linting:

```bash
# Validate Docker compose syntax
docker compose config --quiet

# Validate Prometheus alert rules and syntax with promtool
docker run --rm --entrypoint promtool -v "${PWD}/deploy/prometheus:/etc/prometheus" prom/prometheus:v2.54.1 check rules /etc/prometheus/recording_rules.yml /etc/prometheus/alerts.yml
docker run --rm --entrypoint promtool -v "${PWD}/deploy/prometheus:/etc/prometheus" -w /etc/prometheus prom/prometheus:v2.54.1 test rules test_alerts.yml
```

---

## Troubleshooting

- **Database Connection Refused:**
  Ensure the PostgreSQL container is fully healthy before starting the API:
  `docker compose ps postgres`
- **Promtool Entrypoint Error in CI:**
  The `prom/prometheus` image defaults to `/bin/prometheus`. Always include `--entrypoint promtool` when running verification commands.
- **Port Conflicts (e.g. port 8080 or 5433 already in use):**
  Check active listeners with `Get-NetTCPConnection -LocalPort 8080` (Windows) or `lsof -i :8080` (Linux/macOS) and update port bindings in `.env`.
- **Resetting Demo Data:**
  To wipe transient test reservations and re-seed clean demonstration events, re-run `./scripts/seed_demo.sh` or `.\scripts\seed_demo.ps1`.

---

## Contributing

Contributions are welcome! Please follow these steps:
1. Open an issue to discuss proposed changes or features.
2. Create a feature branch (`git checkout -b feat/your-feature-name`).
3. Ensure all static checks, tests, and formatting pass (`go test ./...`, `npm run build`, and `gofmt`).
4. Submit a Pull Request with a clear summary of your changes.
