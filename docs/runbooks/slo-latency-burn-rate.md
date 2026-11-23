# Runbook: Latency SLO Burn Rate

## Symptom
`SLOLatencyBurnRate` is firing. 99th percentile HTTP latency has exceeded 500ms for more than 5 minutes.

## Likely Causes
1. Heavy database lock contention on hot ticket reservation rows (`events` table).
2. Database index missing or slow query sequential scans.
3. Redis rate limiter or cache latency spike due to high CPU load or network throttling.
4. CPU throttling on the `api` container.

## Diagnostic Queries & Commands
- **PromQL (p95/p99 by Route):** `job:orderflow_http_duration_seconds:p99`
- **Database lock monitor:** `docker compose exec postgres psql -U orderflow -d orderflow -c "SELECT pid, locktype, relation::regclass, mode, granted FROM pg_locks WHERE NOT granted;"`
- **Slow queries:** `docker compose exec postgres psql -U orderflow -d orderflow -c "SELECT query, calls, total_exec_time FROM pg_stat_statements ORDER BY total_exec_time DESC LIMIT 5;"`

## Mitigation
1. Identify which routes are slow (check `job:orderflow_http_duration_seconds:p99` by `path`).
2. If hot-event row locking is the bottleneck, verify Redis sold-out fast-path is enabled (`ORDERFLOW_SOLDOUT_FASTPATH=true`).
3. Check CPU/memory pressure on host and containers: `docker stats --no-stream`.
4. If Postgres connection pool is saturated, scale worker pool sizing or restart unresponsive connections.
