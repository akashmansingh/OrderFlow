DROP INDEX IF EXISTS outbox_events_claim_idx;

ALTER TABLE outbox_events
    DROP COLUMN IF EXISTS locked_at;
