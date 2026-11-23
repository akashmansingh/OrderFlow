# Runbook: Stuck Sagas

## Symptom
`StuckSagas` alert is firing. The oldest pending record in `outbox_events` has remained unpublished for > 30 seconds (`orderflow_outbox_oldest_row_age_seconds > 30`).

## Likely Causes
1. The outbox relay inside `worker` is paused, deadlocked, or crashed.
2. Redpanda broker is unreachable or rejecting producer messages (disk full, broker leader election in progress).
3. Database lock or long-running transaction blocking `SELECT ... FOR UPDATE SKIP LOCKED` on the outbox table.

## Diagnostic Queries & Commands
- **PromQL:** `orderflow_outbox_oldest_row_age_seconds` and `orderflow_outbox_backlog_records`
- **SQL check:** `docker compose exec postgres psql -U orderflow -d orderflow -c "SELECT id, event_type, created_at, retry_count, last_error FROM outbox_events WHERE published_at IS NULL ORDER BY created_at ASC LIMIT 10;"`
- **Worker logs:** `docker compose logs --tail=100 worker | grep -i "relay"`

## Mitigation
1. Check if the worker is running: `docker compose ps worker`. If down, restart: `docker compose restart worker`.
2. Inspect worker logs for publisher errors: `docker compose logs worker | grep -E "publish|broker|kafka"`.
3. Check Redpanda health: `docker compose exec redpanda rpk cluster status`.
4. If outbox events are failing with repeated errors, check `last_error` in `outbox_events` to resolve transient broker rejections.
