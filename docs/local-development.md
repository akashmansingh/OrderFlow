# Local Development

## Prerequisites

- Go 1.27 or later
- Docker Desktop with Docker Compose enabled
- Git

Verify the Docker installation from the repository root:

```powershell
docker --version
docker compose version
```

## Configure local values

The checked-in Compose defaults are safe only for local development. Create a personal environment file before changing values:

```powershell
Copy-Item .env.example .env
```

`.env` is ignored by Git. Do not use the example passwords outside local development.

Replace the JWT placeholder with a locally generated value before starting the API. The API requires at least 32 bytes and refuses to start without a valid secret:

```powershell
$jwtSecret = -join ((1..48) | ForEach-Object { '{0:X2}' -f (Get-Random -Maximum 256) })
(Get-Content .env) -replace '^JWT_SECRET=.*', "JWT_SECRET=$jwtSecret" | Set-Content .env
```

Keep this value out of source control. Only the API container receives `JWT_SECRET`; the workflow worker does not need the signing key.

## Start PostgreSQL, pgAdmin, Redis, and Redpanda

Start dependencies from the repository root:

```powershell
docker compose up -d postgres pgadmin redis redpanda
docker compose ps
```

The services are bound to `127.0.0.1`, so they are not exposed to other machines on the network.

| Service | Address | Purpose |
| --- | --- | --- |
| PostgreSQL | `localhost:5433` | Authoritative application data |
| pgAdmin | `http://localhost:5050` | PostgreSQL administration UI |
| Redis | `localhost:6379` | Holds and rate-limiting support |
| Redpanda | `localhost:19092` | Kafka-compatible local broker |

## Redis rate limiting and caching

Redis accelerates sold-out checks and enforces sliding-window rate limiting on sensitive HTTP endpoints:
- `POST /v1/auth/login`: limited by client IP (`RATE_LIMIT_LOGIN_PER_MINUTE`, default 5 req/min).
- `POST /v1/events/{id}/reservations`: limited by user token subject or IP (`RATE_LIMIT_RESERVATION_PER_MINUTE`, default 10 req/min).

Rate-limited requests receive HTTP `429 Too Many Requests` with `X-RateLimit-Limit`, `X-RateLimit-Remaining`, and `Retry-After` headers.

If Redis stops or becomes unreachable, the rate limiter fails open (logs a warning and allows traffic through) and the reservation service falls back directly to PostgreSQL.

## Register PostgreSQL in pgAdmin

1. Open `http://localhost:5050`.
2. Sign in with `admin@orderflow.example` and `local-dev-password`, unless you changed those values in `.env`.
3. Select **Add New Server**.
4. Set **Name** to `OrderFlow Local`.
5. On the **Connection** tab, set **Host name/address** to `postgres`, **Port** to `5432`, **Maintenance database** to `orderflow`, **Username** to `orderflow`, and **Password** to `local-dev-password`.
6. Save. Use `postgres`, not `localhost`, because pgAdmin runs inside the same Compose network as PostgreSQL. Tools running directly on your computer use `localhost:5433`.

If you use a separately installed pgAdmin desktop application instead of the Compose pgAdmin service, set the host to `localhost` and port to `5433`.

The Compose migration service applies the versioned schema automatically before the API starts. To apply it without starting the API, run:

```powershell
docker compose run --rm migrate
```

Apply schema changes through migration files and this runner rather than pasting SQL manually into pgAdmin.

## Run and stop the stack

When the API is available, start the complete stack with:

```powershell
docker compose up --build
```

Stop containers while preserving database data:

```powershell
docker compose down
```

To remove OrderFlow's local containers and named volumes, which permanently deletes its local PostgreSQL, Redis, and pgAdmin data:

```powershell
docker compose down --volumes
```

Use the volume-removal command only when you intend to reset this project's local data.

## Workflow worker

The Compose stack starts the worker after database migrations. It relays committed PostgreSQL outbox records to the `reservation-events` Redpanda topic, then processes held reservations asynchronously.

Inspect worker activity with:

```powershell
docker compose logs --follow worker
```

Use `SIMULATED_PAYMENT_RESULT=reject` in your local `.env` to demonstrate cancellation and inventory release. Keep the default `approve` for the normal flow.

### Pay now (checkout mode)

By default (`PAYMENT_MODE=auto`) the worker settles every hold itself. To let the customer drive the payment, start the stack with:

```powershell
$env:PAYMENT_MODE = "checkout"; docker compose up -d --build --wait
```

Holds then stay open until `POST /v1/reservations/{id}/payment` is called (the web UI's "Pay now" button does this). The worker waits `PAYMENT_PROCESSING_SECONDS` (default 3 in Compose) before settling, so the "Processing" state is visible. A simulated decline keeps the hold open for a retry. Return to `auto` before running load tests or benchmarks. The opt-in e2e tests in `tests/e2e/payment_test.go` need checkout mode and skip otherwise.

## Authenticate API requests

Register and log in before calling event or reservation routes:

```powershell
$credentials = @{ email = "local@example.com"; password = "correct horse battery" } | ConvertTo-Json
Invoke-RestMethod http://127.0.0.1:8080/v1/auth/register -Method Post -Body $credentials -ContentType "application/json"
$token = (Invoke-RestMethod http://127.0.0.1:8080/v1/auth/login -Method Post -Body $credentials -ContentType "application/json").access_token
$headers = @{ Authorization = "Bearer $token"; "Idempotency-Key" = [guid]::NewGuid().ToString() }
```

Pass `$headers` with protected requests. Reservation creation takes only `quantity`; the API derives `customer_id` from the token. Reservation status reads are limited to the token's user.
