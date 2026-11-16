# OrderFlow

Go ticket reservation backend focused on safe inventory allocation under contention and a recoverable asynchronous workflow.

The project is at the architecture/setup stage. See [the architecture](docs/architecture.md), [repository instructions](AGENTS.md), and [workspace working rules](docs/WORKSPACE_RULES.md) before implementation.

## Initial direction

- Reservation API and one workflow worker, both in Go
- PostgreSQL as the source of truth, Redis for short-lived holds/rate limits
- Transactional outbox and Redpanda (Kafka API) for asynchronous events
- Simulated payment; at-least-once delivery with idempotent processing
- Docker Compose local environment and initial single-VM deployment

Detailed setup commands will be added alongside the first runnable implementation.
