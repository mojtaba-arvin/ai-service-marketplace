-- TRUST-002: tenant-bound order incident intake with reporter-only access.
-- Tenant scope is anchored to the persisted order, never to request input.
-- The unique index is idempotent for legacy installations without migration history.
CREATE UNIQUE INDEX IF NOT EXISTS orders_id_tenant_unique
    ON orders(id,tenant_id);

CREATE TABLE IF NOT EXISTS incident_reports (
    id text PRIMARY KEY,
    order_id text NOT NULL,
    tenant_id text NOT NULL CHECK (length(btrim(tenant_id)) BETWEEN 1 AND 120),
    reporter_identity_id text NOT NULL REFERENCES phone_identities(id) ON DELETE RESTRICT,
    reporter_role text NOT NULL CHECK (reporter_role IN ('customer','professional')),
    category text NOT NULL CHECK (category ~ '^[a-z][a-z0-9_]{1,49}$'),
    narrative text NOT NULL CHECK (length(btrim(narrative)) BETWEEN 10 AND 4000),
    occurred_at timestamptz,
    severity_hint text NOT NULL DEFAULT 'unknown'
        CHECK (severity_hint IN ('unknown','low','high','critical')),
    status text NOT NULL DEFAULT 'submitted'
        CHECK (status IN ('submitted','triaged','investigating','resolved','closed')),
    evidence_refs jsonb NOT NULL DEFAULT '[]'::jsonb
        CHECK (jsonb_typeof(evidence_refs)='array' AND jsonb_array_length(evidence_refs)<=5),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT incident_reports_order_tenant_fk
        FOREIGN KEY (order_id,tenant_id)
        REFERENCES orders(id,tenant_id) ON DELETE RESTRICT,
    CONSTRAINT incident_reports_id_tenant_unique UNIQUE (id,tenant_id)
);

CREATE INDEX IF NOT EXISTS incident_reports_reporter_idx
    ON incident_reports(tenant_id,reporter_identity_id,created_at DESC,id DESC);
CREATE INDEX IF NOT EXISTS incident_reports_order_idx
    ON incident_reports(tenant_id,order_id,created_at DESC,id DESC);

CREATE TABLE IF NOT EXISTS incident_report_events (
    id bigserial PRIMARY KEY,
    report_id text NOT NULL,
    tenant_id text NOT NULL,
    actor_identity_id text NOT NULL REFERENCES phone_identities(id) ON DELETE RESTRICT,
    action text NOT NULL CHECK (action IN ('submitted','assigned','severity_changed','status_changed')),
    reason text NOT NULL CHECK (length(btrim(reason)) BETWEEN 1 AND 500),
    before_state jsonb NOT NULL DEFAULT '{}'::jsonb,
    after_state jsonb NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT incident_report_events_report_tenant_fk
        FOREIGN KEY (report_id,tenant_id)
        REFERENCES incident_reports(id,tenant_id) ON DELETE RESTRICT
);
CREATE INDEX IF NOT EXISTS incident_report_events_tenant_idx
    ON incident_report_events(tenant_id,report_id,created_at,id);

CREATE OR REPLACE FUNCTION reject_incident_report_event_mutation()
RETURNS trigger LANGUAGE plpgsql
AS 'BEGIN
    RAISE EXCEPTION ''incident report events are append-only'';
END';
DROP TRIGGER IF EXISTS incident_report_events_immutable ON incident_report_events;
CREATE TRIGGER incident_report_events_immutable
BEFORE UPDATE OR DELETE ON incident_report_events
FOR EACH ROW EXECUTE FUNCTION reject_incident_report_event_mutation();

CREATE OR REPLACE FUNCTION guard_incident_report_identity()
RETURNS trigger LANGUAGE plpgsql
AS 'BEGIN
    RAISE EXCEPTION ''incident reports are immutable until audited reviewer transitions are implemented'';
END';
DROP TRIGGER IF EXISTS incident_reports_identity_guard ON incident_reports;
CREATE TRIGGER incident_reports_identity_guard
BEFORE UPDATE OR DELETE ON incident_reports
FOR EACH ROW EXECUTE FUNCTION guard_incident_report_identity();
