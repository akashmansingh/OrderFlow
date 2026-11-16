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

## Register PostgreSQL in pgAdmin

1. Open `http://localhost:5050`.
2. Sign in with `admin@orderflow.local` and `local-dev-password`, unless you changed those values in `.env`.
3. Select **Add New Server**.
4. Set **Name** to `OrderFlow Local`.
5. On the **Connection** tab, set **Host name/address** to `postgres`, **Port** to `5432`, **Maintenance database** to `orderflow`, **Username** to `orderflow`, and **Password** to `local-dev-password`.
6. Save. Use `postgres`, not `localhost`, because pgAdmin runs inside the same Compose network as PostgreSQL. Tools running directly on your computer use `localhost:5433`.

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
