ALTER TABLE outbox_events
    ADD COLUMN IF NOT EXISTS attempts integer NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    ADD COLUMN IF NOT EXISTS next_attempt_at timestamptz NOT NULL DEFAULT now(),
    ADD COLUMN IF NOT EXISTS locked_by text,
    ADD COLUMN IF NOT EXISTS locked_until timestamptz,
    ADD COLUMN IF NOT EXISTS last_error text,
    ADD COLUMN IF NOT EXISTS dead_lettered_at timestamptz;

CREATE INDEX IF NOT EXISTS outbox_claim_idx
    ON outbox_events(next_attempt_at,id)
    WHERE published_at IS NULL AND dead_lettered_at IS NULL;

CREATE INDEX IF NOT EXISTS outbox_lease_idx
    ON outbox_events(locked_until)
    WHERE published_at IS NULL AND locked_by IS NOT NULL;

ALTER TABLE processed_events
    ADD CONSTRAINT processed_events_event_id_consumer_unique UNIQUE(consumer_name,event_id);
