# Runbook: Dead-Letter Queue Active

## Symptom
`DeadLetterQueueActive` is firing. Poison or unrecoverable events are actively routing to `reservation-events-dlq`.

## Likely Causes
1. Schema mismatch between event producer and consumer.
2. Malformed JSON payload or invalid foreign keys/UUIDs.
3. Event exceeded maximum retry attempts (3 retries).

## Diagnostic Queries & Commands
- **PromQL:** `sum by (reason, event_type) (rate(orderflow_worker_dlq_events_total[5m]))`
- **Consume DLQ messages:** `docker compose exec redpanda rpk topic consume reservation-events-dlq -n 5`
- **Worker error logs:** `docker compose logs worker | grep -i "dlq"`

## Mitigation
1. Read the most recent DLQ messages using `rpk topic consume reservation-events-dlq` to inspect the error header and payload.
2. Identify whether the root cause is a schema change, poison payload, or downstream dependency outage.
3. Fix the producer schema regression or apply a consumer patch to handle the payload format.
