ALTER TABLE outbox_events
    ADD COLUMN locked_at TIMESTAMPTZ;

CREATE INDEX outbox_events_claim_idx
    ON outbox_events (occurred_at)
    WHERE published_at IS NULL;
