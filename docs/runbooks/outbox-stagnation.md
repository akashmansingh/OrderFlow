# Runbook: Outbox Stagnation

## Symptom
`OutboxStagnation` alert is firing. The number of unpublished outbox events exceeds 100 (`orderflow_outbox_backlog_records > 100`) for > 2 minutes.

## Likely Causes
1. Ingestion rate of new reservations exceeds outbox relay throughput.
2. Redpanda broker partition leader election or network partition.
3. Relay polling interval is too long or batch size is too small.

## Diagnostic Queries & Commands
- **PromQL:** `orderflow_outbox_backlog_records`
- **SQL count:** `docker compose exec postgres psql -U orderflow -d orderflow -c "SELECT count(*) FROM outbox_events WHERE published_at IS NULL;"`
- **Broker produce throughput:** `sum(rate(redpanda_kafka_request_latency_seconds_count{type="produce"}[1m]))`

## Mitigation
1. Verify broker responsiveness: `docker compose exec redpanda rpk topic describe reservation-events`.
2. Inspect outbox store contention in Postgres: ensure index on `outbox_events(published_at, created_at)` is being utilized.
3. Restart or scale worker processes if the relay is healthy but overloaded.
