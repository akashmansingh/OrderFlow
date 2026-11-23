# ADR 0003: Prometheus Metrics & Dead-Letter Queue (DLQ) Resilience

## Status

Accepted

## Context

OrderFlow processes high-contention ticket reservations asynchronously using a transactional outbox and Redpanda event broker. To operate predictably and provide verifiable operational evidence:
1. Operational metrics (request latencies, error rates, outbox backlog, cache hit/miss rates, and worker outcomes) must be scrapeable by Prometheus.
2. Poison messages (malformed payloads or schema violations) or repeatedly failing events must not stall Kafka consumer group progress or cause cascading crashes.
3. State transitions for successful payments must be normalized to canonical `confirmed` status.

## Decisions

1. **Prometheus Metrics Standard:**
   - Adopt `github.com/prometheus/client_golang` using custom `prometheus.Registry` instances to avoid global state collisions in tests.
   - Expose `GET /metrics` on the API server.
   - Collect request duration histograms, rate limit rejections, outbox backlog gauge, Redis cache/fail-open metrics, and worker event processing outcomes.

2. **Dead-Letter Queue (DLQ) Handling:**
   - Differentiate transient errors (retried up to 3 times with backoff) from unrecoverable poison errors (immediately sent to DLQ).
   - Produce dead-lettered events to `reservation-events-dlq` topic with diagnostic metadata (error reason, failure timestamp, attempt count).
   - Acknowledge/commit original consumer offsets once published to the DLQ to prevent partition blockages.

3. **Status Normalization:**
   - Update `internal/workflow/processor.go` so approved payments transition to `confirmed` status, matching `reservation.confirmed.v1` and API representations.

## Consequences

- Full operational visibility into system performance and failure rates via standard Prometheus scrapers.
- Resilient message consumption where bad messages are safely captured for inspection without disrupting healthy reservation flow.
- Minimal overhead (< 1% CPU) using Prometheus histograms with bounded buckets.
