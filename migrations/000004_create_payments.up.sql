-- One row per payment attempt against a held reservation. A declined attempt
-- leaves the reservation held, so a reservation can have several attempts.
CREATE TABLE payments (
    id UUID PRIMARY KEY,
    reservation_id UUID NOT NULL REFERENCES reservations(id),
    customer_id UUID NOT NULL,
    -- Simulated outcome chosen by the client. No card data is ever stored.
    scenario TEXT NOT NULL CHECK (scenario IN ('approve', 'decline')),
    status TEXT NOT NULL CHECK (status IN ('processing', 'succeeded', 'declined')),
    idempotency_key TEXT NOT NULL CHECK (idempotency_key <> ''),
    failure_reason TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    completed_at TIMESTAMPTZ,
    CONSTRAINT payments_completion_consistent CHECK ((status = 'processing') = (completed_at IS NULL)),
    CONSTRAINT payments_failure_reason_consistent CHECK ((status = 'declined') = (failure_reason IS NOT NULL))
);

-- A retried request with the same key resolves to the same attempt.
CREATE UNIQUE INDEX payments_reservation_idempotency_key_idx ON payments (reservation_id, idempotency_key);

-- At most one attempt in flight, and at most one success, per reservation.
CREATE UNIQUE INDEX payments_one_processing_idx ON payments (reservation_id) WHERE status = 'processing';
CREATE UNIQUE INDEX payments_one_succeeded_idx ON payments (reservation_id) WHERE status = 'succeeded';

CREATE INDEX payments_reservation_created_idx ON payments (reservation_id, created_at DESC);
