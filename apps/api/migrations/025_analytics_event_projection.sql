-- Analytics projections store only validated, minimized canonical event facts.
-- They deliberately do not FK-reference outbox rows: source payload retention
-- and deletion must remain governed by the outbox/privacy retention policy.
CREATE TABLE IF NOT EXISTS analytics_event_facts (
    tenant_id text NOT NULL,
    event_id text NOT NULL,
    outbox_event_id bigint NOT NULL,
    event_type text NOT NULL,
    event_version integer NOT NULL CHECK (event_version > 0),
    occurred_at timestamptz NOT NULL,
    aggregate_type text NOT NULL,
    aggregate_id text NOT NULL,
    market_code char(2),
    traffic_class text CHECK (traffic_class IS NULL OR traffic_class IN ('production','demo','test')),
    data jsonb NOT NULL CHECK (jsonb_typeof(data) = 'object'),
    payload_sha256 char(64) NOT NULL,
    projected_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id,event_id),
    UNIQUE (outbox_event_id),
    CHECK (market_code IS NULL OR market_code ~ '^[A-Z]{2}$')
);

CREATE INDEX IF NOT EXISTS analytics_event_facts_tenant_time_idx
    ON analytics_event_facts (tenant_id,occurred_at,event_type);
CREATE INDEX IF NOT EXISTS analytics_event_facts_tenant_market_time_idx
    ON analytics_event_facts (tenant_id,market_code,occurred_at);
CREATE INDEX IF NOT EXISTS analytics_event_facts_order_lookup_idx
    ON analytics_event_facts (tenant_id,event_type,market_code,(data->>'order_id'),occurred_at);
CREATE INDEX IF NOT EXISTS analytics_event_facts_offer_lookup_idx
    ON analytics_event_facts (tenant_id,event_type,market_code,(data->>'offer_id'),occurred_at);

CREATE TABLE IF NOT EXISTS analytics_event_quarantine (
    outbox_event_id bigint PRIMARY KEY,
    event_type text NOT NULL,
    aggregate_type text NOT NULL,
    aggregate_id text NOT NULL,
    reason_code text NOT NULL,
    payload_sha256 char(64) NOT NULL,
    quarantined_at timestamptz NOT NULL DEFAULT now()
);

CREATE OR REPLACE FUNCTION reject_analytics_projection_mutation()
RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'analytics projections are append-only; rebuild a new projection generation';
END;
$$;

DROP TRIGGER IF EXISTS analytics_event_facts_immutable ON analytics_event_facts;
CREATE TRIGGER analytics_event_facts_immutable
    BEFORE UPDATE OR DELETE ON analytics_event_facts
    FOR EACH ROW EXECUTE FUNCTION reject_analytics_projection_mutation();

DROP TRIGGER IF EXISTS analytics_event_quarantine_immutable ON analytics_event_quarantine;
CREATE TRIGGER analytics_event_quarantine_immutable
    BEFORE UPDATE OR DELETE ON analytics_event_quarantine
    FOR EACH ROW EXECUTE FUNCTION reject_analytics_projection_mutation();
