-- Preserve source event time separately from the durable outbox ingestion time.
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
    ADD COLUMN IF NOT EXISTS included_in_metrics boolean NOT NULL DEFAULT true;

UPDATE analytics_event_facts f
SET ingested_at = COALESCE(
        (SELECT e.created_at FROM outbox_events e WHERE e.id=f.outbox_event_id),
        f.projected_at
    ),
    event_time_family = CASE
        WHEN f.event_type LIKE 'provider.%' THEN 'provider_supply'
        WHEN f.event_type LIKE 'review.%' OR f.event_type LIKE 'trust.%' THEN 'service_quality'
        WHEN f.event_type LIKE 'finance.%' OR f.event_type LIKE 'ledger.%' THEN 'finance'
        ELSE 'order_funnel'
    END
WHERE f.ingested_at IS NULL;

ALTER TABLE analytics_event_facts ALTER COLUMN ingested_at SET NOT NULL;
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
    ADD COLUMN IF NOT EXISTS event_time_family text;

UPDATE analytics_event_quarantine q
SET event_id = e.payload->>'id',
    event_version = CASE
        WHEN e.payload->>'version' ~ '^[0-9]{1,9}$' THEN (e.payload->>'version')::integer
        ELSE NULL
    END,
    ingested_at = e.created_at,
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
COMMENT ON COLUMN analytics_event_quarantine.event_id IS 'Canonical source event ID when the envelope can be decoded.';
COMMENT ON COLUMN analytics_event_quarantine.event_version IS 'Canonical source schema version when the envelope can be decoded.';
COMMENT ON COLUMN analytics_event_quarantine.occurred_at IS 'Producer-supplied event time when the envelope can be decoded.';
COMMENT ON COLUMN analytics_event_quarantine.ingested_at IS 'Persisted outbox created_at ingestion provenance.';
COMMENT ON COLUMN analytics_event_quarantine.market_code IS 'Canonical market attribution when validated; null remains incomplete.';
