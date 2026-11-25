-- Generation-scoped, append-only analytics projections.
CREATE TABLE IF NOT EXISTS analytics_projection_generations (
 tenant_id text NOT NULL, generation_id text NOT NULL, consumer_name text NOT NULL UNIQUE,
 status text NOT NULL CHECK(status IN ('building','active','retired')),
 created_at timestamptz NOT NULL DEFAULT now(), activated_at timestamptz, retired_at timestamptz,
 PRIMARY KEY(tenant_id,generation_id),
 CHECK((status='building' AND activated_at IS NULL AND retired_at IS NULL) OR
       (status='active' AND activated_at IS NOT NULL AND retired_at IS NULL) OR
       (status='retired' AND activated_at IS NOT NULL AND retired_at IS NOT NULL))
);
CREATE TABLE IF NOT EXISTS analytics_active_generations (
 tenant_id text PRIMARY KEY, generation_id text NOT NULL, activated_at timestamptz NOT NULL DEFAULT now(),
 FOREIGN KEY(tenant_id,generation_id) REFERENCES analytics_projection_generations(tenant_id,generation_id) ON DELETE RESTRICT
);
CREATE TABLE IF NOT EXISTS analytics_event_facts (
 tenant_id text NOT NULL, generation_id text NOT NULL, event_id text NOT NULL, outbox_event_id bigint NOT NULL,
 event_type text NOT NULL, event_version integer NOT NULL CHECK(event_version>0), occurred_at timestamptz NOT NULL,
 aggregate_type text NOT NULL, aggregate_id text NOT NULL, market_code char(2),
 traffic_class text CHECK(traffic_class IS NULL OR traffic_class IN ('production','demo','test')),
 data jsonb NOT NULL CHECK(jsonb_typeof(data)='object'), payload_sha256 char(64) NOT NULL,
 projected_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(tenant_id,generation_id,event_id), UNIQUE(tenant_id,generation_id,outbox_event_id),
 FOREIGN KEY(tenant_id,generation_id) REFERENCES analytics_projection_generations(tenant_id,generation_id) ON DELETE RESTRICT,
 CHECK(market_code IS NULL OR market_code ~ '^[A-Z]{2}$')
);
CREATE INDEX IF NOT EXISTS analytics_event_facts_tenant_generation_time_idx ON analytics_event_facts(tenant_id,generation_id,occurred_at,event_type);
CREATE INDEX IF NOT EXISTS analytics_event_facts_generation_market_time_idx ON analytics_event_facts(tenant_id,generation_id,market_code,occurred_at);
CREATE INDEX IF NOT EXISTS analytics_event_facts_generation_order_lookup_idx ON analytics_event_facts(tenant_id,generation_id,event_type,market_code,(data->>'order_id'),occurred_at);
CREATE INDEX IF NOT EXISTS analytics_event_facts_generation_offer_lookup_idx ON analytics_event_facts(tenant_id,generation_id,event_type,market_code,(data->>'offer_id'),occurred_at);
CREATE TABLE IF NOT EXISTS analytics_event_quarantine (
 tenant_id text NOT NULL, generation_id text NOT NULL, outbox_event_id bigint NOT NULL,
 event_type text NOT NULL, aggregate_type text NOT NULL, aggregate_id text NOT NULL, reason_code text NOT NULL,
 payload_sha256 char(64) NOT NULL, quarantined_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(tenant_id,generation_id,outbox_event_id),
 FOREIGN KEY(tenant_id,generation_id) REFERENCES analytics_projection_generations(tenant_id,generation_id) ON DELETE RESTRICT
);
CREATE OR REPLACE FUNCTION reject_analytics_projection_mutation() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN RAISE EXCEPTION 'analytics projections are append-only; rebuild a new projection generation'; END;
$$;
DROP TRIGGER IF EXISTS analytics_event_facts_immutable ON analytics_event_facts;
CREATE TRIGGER analytics_event_facts_immutable BEFORE UPDATE OR DELETE ON analytics_event_facts FOR EACH ROW EXECUTE FUNCTION reject_analytics_projection_mutation();
DROP TRIGGER IF EXISTS analytics_event_facts_no_truncate ON analytics_event_facts;
CREATE TRIGGER analytics_event_facts_no_truncate BEFORE TRUNCATE ON analytics_event_facts FOR EACH STATEMENT EXECUTE FUNCTION reject_analytics_projection_mutation();
DROP TRIGGER IF EXISTS analytics_event_quarantine_immutable ON analytics_event_quarantine;
CREATE TRIGGER analytics_event_quarantine_immutable BEFORE UPDATE OR DELETE ON analytics_event_quarantine FOR EACH ROW EXECUTE FUNCTION reject_analytics_projection_mutation();
DROP TRIGGER IF EXISTS analytics_event_quarantine_no_truncate ON analytics_event_quarantine;
CREATE TRIGGER analytics_event_quarantine_no_truncate BEFORE TRUNCATE ON analytics_event_quarantine FOR EACH STATEMENT EXECUTE FUNCTION reject_analytics_projection_mutation();
