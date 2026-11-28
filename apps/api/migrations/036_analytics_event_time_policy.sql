-- Preserve source event time separately from the durable outbox ingestion time.
-- Old outbox rows retain their historical transaction-start timestamp but are
-- explicitly marked untrusted for event-time/ingestion-time comparisons.
ALTER TABLE outbox_events
    ADD COLUMN IF NOT EXISTS ingested_at_source text NOT NULL DEFAULT 'legacy_transaction_timestamp';

CREATE OR REPLACE FUNCTION set_outbox_insert_ingestion_time() RETURNS trigger LANGUAGE plpgsql AS $fn$
BEGIN
    NEW.created_at := clock_timestamp();
    NEW.ingested_at_source := 'insert_clock';
    RETURN NEW;
END $fn$;
DROP TRIGGER IF EXISTS outbox_events_ingestion_time ON outbox_events;
CREATE TRIGGER outbox_events_ingestion_time BEFORE INSERT ON outbox_events
FOR EACH ROW EXECUTE FUNCTION set_outbox_insert_ingestion_time();

-- Existing immutable facts are backfilled from their source outbox row before
-- the append-only triggers are restored.
DROP TRIGGER IF EXISTS analytics_event_facts_immutable ON analytics_event_facts;
DROP TRIGGER IF EXISTS analytics_event_facts_no_truncate ON analytics_event_facts;

ALTER TABLE analytics_event_facts
    ADD COLUMN IF NOT EXISTS ingested_at timestamptz,
    ADD COLUMN IF NOT EXISTS event_time_family text NOT NULL DEFAULT 'order_funnel',
    ADD COLUMN IF NOT EXISTS lateness_policy_version text NOT NULL DEFAULT 'event-time-v1',
    ADD COLUMN IF NOT EXISTS allowed_lateness_seconds bigint NOT NULL DEFAULT 259200 CHECK (allowed_lateness_seconds >= 0),
    ADD COLUMN IF NOT EXISTS late_arrival boolean NOT NULL DEFAULT false,
    ADD COLUMN IF NOT EXISTS included_in_metrics boolean NOT NULL DEFAULT true,
    ADD COLUMN IF NOT EXISTS ingestion_time_trusted boolean NOT NULL DEFAULT false;

UPDATE analytics_event_facts f
SET ingested_at = (SELECT e.created_at FROM outbox_events e WHERE e.id=f.outbox_event_id),
    ingestion_time_trusted = COALESCE((SELECT e.ingested_at_source='insert_clock' FROM outbox_events e WHERE e.id=f.outbox_event_id),false),
    event_time_family = CASE
        WHEN f.event_type LIKE 'provider.%' THEN 'provider_supply'
        WHEN f.event_type LIKE 'review.%' OR f.event_type LIKE 'trust.%' THEN 'service_quality'
        WHEN f.event_type LIKE 'finance.%' OR f.event_type LIKE 'ledger.%' THEN 'finance'
        ELSE 'order_funnel'
    END
WHERE f.ingested_at IS NULL
  AND EXISTS (SELECT 1 FROM outbox_events e WHERE e.id=f.outbox_event_id);

CREATE INDEX IF NOT EXISTS analytics_event_facts_ingestion_idx
    ON analytics_event_facts(tenant_id,generation_id,ingested_at,event_time_family);
CREATE INDEX IF NOT EXISTS analytics_event_facts_late_idx
    ON analytics_event_facts(tenant_id,generation_id,occurred_at)
    WHERE late_arrival=true;

CREATE TRIGGER analytics_event_facts_immutable BEFORE UPDATE OR DELETE ON analytics_event_facts
FOR EACH ROW EXECUTE FUNCTION reject_analytics_projection_mutation();
CREATE TRIGGER analytics_event_facts_no_truncate BEFORE TRUNCATE ON analytics_event_facts
FOR EACH STATEMENT EXECUTE FUNCTION reject_analytics_projection_mutation();

-- Quarantine is also a provenance outcome, not a payload discard. Nullable
-- fields preserve compatibility with historical non-canonical rows.
DROP TRIGGER IF EXISTS analytics_event_quarantine_immutable ON analytics_event_quarantine;

ALTER TABLE analytics_event_quarantine
    ADD COLUMN IF NOT EXISTS event_id text,
    ADD COLUMN IF NOT EXISTS event_version integer,
    ADD COLUMN IF NOT EXISTS occurred_at timestamptz,
    ADD COLUMN IF NOT EXISTS ingested_at timestamptz,
    ADD COLUMN IF NOT EXISTS market_code char(2),
    ADD COLUMN IF NOT EXISTS event_time_family text,
    ADD COLUMN IF NOT EXISTS ingestion_time_source text,
    ADD COLUMN IF NOT EXISTS ingestion_time_trusted boolean NOT NULL DEFAULT false;

UPDATE analytics_event_quarantine q
SET event_id = e.payload->>'id',
    event_version = CASE
        WHEN e.payload->>'version' ~ '^[0-9]{1,9}$' THEN (e.payload->>'version')::integer
        ELSE NULL
    END,
    ingested_at = e.created_at,
    ingestion_time_source = e.ingested_at_source,
    ingestion_time_trusted = (e.ingested_at_source='insert_clock'),
    market_code = CASE
        WHEN e.payload->'metadata'->>'market_code' ~ '^[A-Z]{2}$' THEN e.payload->'metadata'->>'market_code'
        ELSE NULL
    END,
    event_time_family = CASE
        WHEN q.event_type LIKE 'provider.%' THEN 'provider_supply'
        WHEN q.event_type LIKE 'review.%' OR q.event_type LIKE 'trust.%' THEN 'service_quality'
        WHEN q.event_type LIKE 'finance.%' OR q.event_type LIKE 'ledger.%' THEN 'finance'
        ELSE 'order_funnel'
    END
FROM outbox_events e
WHERE e.id=q.outbox_event_id AND q.ingested_at IS NULL;

CREATE INDEX IF NOT EXISTS analytics_event_quarantine_time_idx
    ON analytics_event_quarantine(tenant_id,generation_id,occurred_at,event_time_family);

CREATE TRIGGER analytics_event_quarantine_immutable BEFORE UPDATE OR DELETE ON analytics_event_quarantine
FOR EACH ROW EXECUTE FUNCTION reject_analytics_projection_mutation();

COMMENT ON COLUMN analytics_event_facts.occurred_at IS 'Producer-supplied canonical event time; immutable source timestamp.';
COMMENT ON COLUMN analytics_event_facts.ingested_at IS 'Durable outbox row created_at; ingestion provenance, not event time or commit-order cursor.';
COMMENT ON COLUMN analytics_event_facts.included_in_metrics IS 'False for out-of-policy late events until an explicit correction generation replays them.';
COMMENT ON COLUMN analytics_event_facts.ingestion_time_trusted IS 'True only when outbox ingestion time was captured with clock_timestamp at insert; legacy transaction-start timestamps are untrusted.';
COMMENT ON COLUMN analytics_event_quarantine.event_id IS 'Canonical source event ID when the envelope can be decoded.';
COMMENT ON COLUMN analytics_event_quarantine.event_version IS 'Canonical source schema version when the envelope can be decoded.';
COMMENT ON COLUMN analytics_event_quarantine.occurred_at IS 'Producer-supplied event time when the envelope can be decoded.';
COMMENT ON COLUMN analytics_event_quarantine.ingested_at IS 'Persisted outbox created_at ingestion provenance.';
COMMENT ON COLUMN analytics_event_quarantine.ingestion_time_source IS 'Whether the outbox timestamp is an exact insert clock or a legacy transaction-start approximation.';
COMMENT ON COLUMN analytics_event_quarantine.ingestion_time_trusted IS 'True only for exact outbox insert-clock timestamps.';
COMMENT ON COLUMN analytics_event_quarantine.market_code IS 'Canonical market attribution when validated; null remains incomplete.';

-- Identical logical event IDs delivered through distinct outbox rows are
-- accounted for without double-counting or misclassifying them as quarantine.
CREATE TABLE IF NOT EXISTS analytics_event_duplicates (
    tenant_id text NOT NULL,
    generation_id text NOT NULL,
    outbox_event_id bigint NOT NULL,
    canonical_outbox_event_id bigint NOT NULL,
    event_id text NOT NULL,
    event_version integer NOT NULL,
    occurred_at timestamptz NOT NULL,
    ingested_at timestamptz NOT NULL,
    market_code char(2),
    event_time_family text NOT NULL,
    ingestion_time_source text NOT NULL,
    lateness_policy_version text NOT NULL,
    allowed_lateness_seconds bigint NOT NULL CHECK (allowed_lateness_seconds >= 0),
    late_arrival boolean NOT NULL,
    payload_sha256 char(64) NOT NULL,
    recorded_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY(tenant_id,generation_id,outbox_event_id),
    FOREIGN KEY(tenant_id,generation_id) REFERENCES analytics_projection_generations(tenant_id,generation_id) ON DELETE RESTRICT
);
CREATE INDEX IF NOT EXISTS analytics_event_duplicates_identity_idx
    ON analytics_event_duplicates(tenant_id,generation_id,event_id);
DROP TRIGGER IF EXISTS analytics_event_duplicates_immutable ON analytics_event_duplicates;
DROP TRIGGER IF EXISTS analytics_event_duplicates_no_truncate ON analytics_event_duplicates;
CREATE TRIGGER analytics_event_duplicates_immutable BEFORE UPDATE OR DELETE ON analytics_event_duplicates
FOR EACH ROW EXECUTE FUNCTION reject_analytics_projection_mutation();
CREATE TRIGGER analytics_event_duplicates_no_truncate BEFORE TRUNCATE ON analytics_event_duplicates
FOR EACH STATEMENT EXECUTE FUNCTION reject_analytics_projection_mutation();
