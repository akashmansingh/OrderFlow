# Outbox Publisher and Workflow Worker Design

## Intent

Make OrderFlow's existing transactional outbox operational. A held reservation must become `confirmed`, `cancelled`, or `expired` through an asynchronous Go worker while preserving the invariant that confirmed plus held tickets never exceeds event capacity.

The project remains a single-VM portfolio deployment. It gains a real asynchronous boundary without claiming exactly-once delivery, high availability, or real payment processing.

## Approaches considered

1. **One worker binary with two loops — selected.** One loop relays committed PostgreSQL outbox records to Redpanda; the other consumes reservation events and performs the reservation workflow. It gives a small deployment footprint while retaining a real database-to-broker boundary.
2. **Separate relay and workflow services.** This mirrors large production systems more closely but adds deployment, configuration, and observability cost before the behavior is proven.
3. **Synchronous payment confirmation in the Reservation API.** This is simpler but defeats the purpose of the outbox and hides worker crash and duplicate-delivery behavior.

## Selected design

### Processes and configuration

- Add one `cmd/worker` Go process alongside the API.
- Use `github.com/twmb/franz-go/pkg/kgo` to speak the Kafka API exposed by Redpanda.
- Add explicit configuration for Redpanda brokers, topic (`reservation-events`), consumer group (`orderflow-workflow-worker`), polling interval, worker concurrency, and simulated payment outcome.
- Docker Compose starts the worker after migrations and Redpanda readiness. The API continues to use `postgres:5432`; changing the Windows host port to `5433` has no effect on inter-container connections.

### Outbox relay

1. Poll unpublished `outbox_events` in creation order using row locking with `FOR UPDATE SKIP LOCKED`.
2. Publish each event to `reservation-events` with the stable outbox event ID as the message key and header.
3. Set `published_at` only after Redpanda acknowledges the publish.
4. On relay crash after acknowledgement but before updating `published_at`, publish again after restart. This is expected at-least-once delivery.
5. Increment `publish_attempts` on failure and leave the event available for retry. Dead-letter routing is deferred until a measured retry policy is added.

### Workflow worker

1. Consume `reservation.held.v1` events from Redpanda and commit offsets only after the database transaction succeeds.
2. In one PostgreSQL transaction, insert the event ID into `processed_events`. A duplicate key means the event was already handled and may be safely acknowledged.
3. Lock the reservation and its event inventory row. For a still-valid `held` reservation, simulate payment using `SIMULATED_PAYMENT_RESULT` (`approve` by default; `reject` for deterministic failure demos).
4. If a held event arrives after its expiry, mark it `expired` and release held inventory in the same transaction; the scheduled expiry loop remains a fallback for events that have not yet been consumed.
5. On approval, atomically set reservation status to `confirmed`, decrement `held_quantity`, increment `confirmed_quantity`, and add `reservation.confirmed.v1` to the outbox.
6. On rejection, atomically set status to `cancelled`, decrement `held_quantity`, and add `reservation.cancelled.v1` to the outbox.
7. A scheduled expiry loop locks expired held reservations, sets them to `expired`, decrements `held_quantity`, and writes `reservation.expired.v1` through the same outbox path.

### Failure guarantees

- PostgreSQL remains the authority for reservations and inventory.
- The relay and worker are at-least-once. `processed_events`, conditional state transitions, and stable event IDs make duplicate messages harmless.
- The worker does not confirm a reservation after its expiry time.
- Payment is simulated and no external payment credentials are accepted or stored.

## Interfaces and documentation

- Document emitted event envelope fields: `id`, `type`, `aggregate_id`, `occurred_at`, and `payload`.
- Terminal-event payloads use `reservation_id`, `event_id`, `customer_id`, `quantity`, `status`, and `reason`. `reason` is `payment_rejected` for cancellation, `hold_expired` for expiry, and `null` for confirmation.
- The worker acknowledges and ignores terminal events it receives from `reservation-events`; they are published for future consumers and must not trigger a second workflow transition.
- If a valid held event references a reservation that no longer exists, log the stale event and acknowledge it so it cannot block its consumer partition.
- Keep the existing HTTP reservation response asynchronous: `held` means processing is pending; clients query the reservation endpoint for the final status.
- Rewrite the README with project purpose, component diagram, API quick start, local stack commands, ports, test commands, CI/CD behavior, and current limitations.

## Verification

- Unit tests first for outbox selection/publishing decisions, state transitions, duplicate-event handling, payment rejection, and expiry.
- Integration tests cover PostgreSQL plus Redpanda: a held event becomes confirmed, a duplicate message changes inventory once, and a worker restart does not lose committed work.
- The existing E2E suite remains opt-in and is not run unless `ORDERFLOW_E2E_BASE_URL` is supplied.

## Non-goals

- JWT authentication, Redis hold/cache behavior, a DLQ, metrics/tracing, a separate notification service, real payments, and Kubernetes remain later increments.
