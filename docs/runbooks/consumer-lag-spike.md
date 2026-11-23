# Runbook: Consumer Lag Spike

## Symptom
`ConsumerLagSpike` alert is firing. Kafka consumer group lag for `orderflow-workflow-worker` exceeds 50 records for > 2 minutes.

## Likely Causes
1. Burst in new reservations exceeding single-consumer processing throughput.
2. Worker consumer goroutine stalled waiting on slow simulated payment or database transactions.
3. Consumer group rebalance in progress.

## Diagnostic Queries & Commands
- **PromQL:** `orderflow_kafka_consumer_lag`
- **Redpanda CLI check:** `docker compose exec redpanda rpk group describe orderflow-workflow-worker`
- **Worker CPU usage:** `docker stats --no-stream orderflow-worker-1`

## Mitigation
1. Inspect worker logs for processing bottlenecks: `docker compose logs worker --tail=50`.
2. Check if the consumer is committed: verify current offset matches high watermark.
3. If CPU is maxed, scale worker replica count or partitions on the `reservation-events` topic.
