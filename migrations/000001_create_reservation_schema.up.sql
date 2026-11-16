CREATE TYPE reservation_status AS ENUM ('held', 'confirmed', 'cancelled', 'expired');

CREATE TABLE events (
    id UUID PRIMARY KEY,
    name TEXT NOT NULL,
    starts_at TIMESTAMPTZ NOT NULL,
    capacity INTEGER NOT NULL CHECK (capacity > 0),
    held_quantity INTEGER NOT NULL DEFAULT 0 CHECK (held_quantity >= 0),
    confirmed_quantity INTEGER NOT NULL DEFAULT 0 CHECK (confirmed_quantity >= 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CHECK (held_quantity + confirmed_quantity <= capacity)
);

CREATE TABLE reservations (
    id UUID PRIMARY KEY,
    event_id UUID NOT NULL REFERENCES events(id),
    customer_id UUID NOT NULL,
    quantity INTEGER NOT NULL CHECK (quantity > 0),
    status reservation_status NOT NULL,
    expires_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CHECK (
        (status = 'held' AND expires_at IS NOT NULL)
        OR (status <> 'held')
    )
);

CREATE INDEX reservations_event_id_status_idx ON reservations (event_id, status);
CREATE INDEX reservations_held_expiry_idx ON reservations (expires_at) WHERE status = 'held';

CREATE TABLE idempotency_keys (
    key TEXT PRIMARY KEY,
    request_hash BYTEA NOT NULL,
    reservation_id UUID NOT NULL REFERENCES reservations(id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE outbox_events (
    id UUID PRIMARY KEY,
    aggregate_id UUID NOT NULL,
    event_type TEXT NOT NULL,
    payload JSONB NOT NULL,
    occurred_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    published_at TIMESTAMPTZ,
    publish_attempts INTEGER NOT NULL DEFAULT 0 CHECK (publish_attempts >= 0)
);

CREATE INDEX outbox_events_unpublished_idx ON outbox_events (occurred_at) WHERE published_at IS NULL;

CREATE TABLE processed_events (
    event_id UUID PRIMARY KEY,
    processed_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
