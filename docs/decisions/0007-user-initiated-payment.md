# ADR 0007: User-Initiated "Pay now" Payment Step

## Status

Accepted

## Context

Until now the worker settled every hold on its own: `reservation.held.v1` was consumed and the simulated payment outcome came from the `SIMULATED_PAYMENT_RESULT` environment variable. A customer watching a hold saw "Tickets held. Processing payment asynchronously…" without ever doing anything, so the flow did not show how a payment travels from a person through the API, the outbox and the worker.

The change must keep the existing guarantees (no oversell, idempotent writes, at-least-once delivery handled by an inbox) and must not weaken the existing benchmark evidence, which was measured with automatic settlement. AGENTS.md also requires that payment stays simulated: no card data, no real money.

## Decisions

1. **Two modes, `PAYMENT_MODE=auto|checkout`.** `auto` (default) keeps the original behaviour byte-for-byte. `checkout` makes the hold wait for the customer. Default stays `auto` so existing Compose runs, load tests and `docs/PERFORMANCE.md` remain valid. The API and worker must run in the same mode.

2. **New endpoint** `POST /v1/reservations/{id}/payment` (Bearer, `Idempotency-Key` required, body `{"scenario":"approve"|"decline"}`, default `approve`). It answers `202 Accepted` with the reservation. The scenario is a labelled test outcome, not a card; unknown fields are rejected so card details cannot be sent by accident.

3. **A `payments` table** (migration 000004) records every attempt: `processing`, `succeeded` or `declined`. Partial unique indexes allow at most one in-flight and one succeeded attempt per reservation, and `(reservation_id, idempotency_key)` is unique. CHECK constraints tie `status` to `completed_at` and `failure_reason`. The database is the safety net, not only application code.

4. **The attempt is asynchronous.** The API inserts the attempt and a `payment.requested.v1` outbox event in one transaction and returns. The worker consumes it, locks reservation then payment, and in one transaction settles the attempt, transitions the reservation and writes the outcome events (`payment.succeeded.v1` or `payment.declined.v1`, plus the existing `reservation.confirmed.v1`/`reservation.expired.v1`). Duplicate delivery is handled by the existing `processed_events` inbox plus the `processing` state guard.

5. **A decline does not cancel the hold.** The attempt is `declined` with `failure_reason=card_declined`; the reservation stays `held` with its countdown running, and the customer may retry with a new Idempotency-Key. Replaying the same key returns the current state and never starts a second attempt. A different key while one attempt is in flight gets `409`.

6. **Expiry always wins.** A payment arriving after the hold lapsed is declined with `hold_expired`. If the worker sees a lapsed hold first it expires it itself. The sweeper settles any in-flight attempt of a reservation it expires, so no attempt stays `processing` forever and a late `payment.requested.v1` becomes a no-op.

7. **Lock order is reservation → payment** in the API, worker and sweeper, so they cannot deadlock.

8. **Processing delay.** `PAYMENT_PROCESSING_SECONDS` (default 0, Compose sets 3 for the worker) sleeps before the settle transaction, outside any transaction, so the UI can show a "Processing" state.

9. **API shape.** Reservations gain an optional `payment` object `{status: awaiting|processing|declined|succeeded, attempts, payment_id?, failure_reason?}`, present only in checkout mode.

## Consequences

- The `cancelled` status is only produced in `auto` mode; in `checkout` mode a failed hold ends as `expired`.
- The delay is a blocking sleep inside the single-threaded consume loop, so one worker handles roughly one payment per delay period. Acceptable for a demo; do not enable a delay under load tests. A real design would use a scheduled/delayed event or a concurrent handler.
- Production Compose (`deploy/compose.prod.yaml`) is unchanged and stays in `auto`.
- A customer cannot yet release a hold voluntarily; a "cancel hold" endpoint is a natural follow-up.
- The Go code in this change was written without a working Go toolchain in the authoring environment; the SQL logic was exercised on PostgreSQL 16. CI is the first compile and test run.
