# Runbook: PostgreSQL High Connections

## Symptom
`PostgresConnectionsHigh` is firing. PostgreSQL active connections exceed 80% of `max_connections` for > 3 minutes.

## Likely Causes
1. Connection leak in application code (connections acquired from pool but not released).
2. Surge in concurrent API or worker instances.
3. Long-running queries holding connections open.

## Diagnostic Queries & Commands
- **PromQL:** `pg_stat_database_numbackends / pg_settings_max_connections`
- **Active connections by state:** `docker compose exec postgres psql -U orderflow -d orderflow -c "SELECT state, count(*) FROM pg_stat_activity GROUP BY state;"`
- **Longest running queries:** `docker compose exec postgres psql -U orderflow -d orderflow -c "SELECT pid, now() - query_start AS duration, query FROM pg_stat_activity WHERE state != 'idle' ORDER BY duration DESC LIMIT 5;"`

## Mitigation
1. Check for hanging queries: terminate queries running longer than 30s using `SELECT pg_terminate_backend(pid);`.
2. Inspect application pgx connection pool sizes (`internal/database/postgres.go` default pool max).
3. If legitimate traffic surge, tune `max_connections` in `postgresql.conf` and restart Postgres.
