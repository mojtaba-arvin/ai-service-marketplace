-- Private incident evidence metadata and append-only access audit.
-- Evidence bytes remain in an encrypted private adapter; no public URL is persisted.
CREATE TABLE IF NOT EXISTS incident_evidence (
    id text PRIMARY KEY CHECK (id ~ '^[a-f0-9]{32}$'),
    report_id text NOT NULL,
    tenant_id text NOT NULL,
    reporter_identity_id text NOT NULL,
    media_type text NOT NULL CHECK (length(media_type) BETWEEN 1 AND 120),
    size_bytes bigint NOT NULL CHECK (size_bytes > 0),
    retention_expires_at timestamptz,
    legal_hold boolean NOT NULL DEFAULT false,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT incident_evidence_report_tenant_fk
        FOREIGN KEY (report_id,tenant_id)
        REFERENCES incident_reports(id,tenant_id) ON DELETE RESTRICT
);
CREATE INDEX IF NOT EXISTS incident_evidence_report_idx
    ON incident_evidence(tenant_id,report_id,created_at,id);

CREATE TABLE IF NOT EXISTS incident_evidence_access_events (
    id bigserial PRIMARY KEY,
    tenant_id text NOT NULL,
    report_id text NOT NULL,
    evidence_id text NOT NULL,
    actor_identity_id text NOT NULL DEFAULT '',
    action text NOT NULL CHECK (action IN ('upload','download')),
    outcome text NOT NULL CHECK (outcome IN ('success','denied')),
    reason_category text NOT NULL CHECK (reason_category IN (
        'accepted','unauthorized','not_found','invalid_content','too_large',
        'limit_reached','scanner_unavailable','scanner_rejected','storage_failure',
        'key_failure','retention_expired','audit_failure'
    )),
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS incident_evidence_access_report_idx
    ON incident_evidence_access_events(tenant_id,report_id,created_at,id);

CREATE OR REPLACE FUNCTION reject_incident_evidence_access_event_mutation()
RETURNS trigger LANGUAGE plpgsql
AS 'BEGIN
    RAISE EXCEPTION ''incident evidence access events are append-only'';
END';
DROP TRIGGER IF EXISTS incident_evidence_access_events_immutable ON incident_evidence_access_events;
CREATE TRIGGER incident_evidence_access_events_immutable
BEFORE UPDATE OR DELETE ON incident_evidence_access_events
FOR EACH ROW EXECUTE FUNCTION reject_incident_evidence_access_event_mutation();

-- Evidence metadata is immutable except for explicit adopter-managed retention
-- and legal-hold controls. No automatic deletion is introduced by this migration.
CREATE OR REPLACE FUNCTION guard_incident_evidence_metadata()
RETURNS trigger LANGUAGE plpgsql
AS 'BEGIN
    IF TG_OP = ''DELETE'' THEN
        RAISE EXCEPTION ''incident evidence metadata cannot be deleted without a reviewed retention workflow'';
    END IF;
    IF OLD.id IS DISTINCT FROM NEW.id
       OR OLD.report_id IS DISTINCT FROM NEW.report_id
       OR OLD.tenant_id IS DISTINCT FROM NEW.tenant_id
       OR OLD.reporter_identity_id IS DISTINCT FROM NEW.reporter_identity_id
       OR OLD.media_type IS DISTINCT FROM NEW.media_type
       OR OLD.size_bytes IS DISTINCT FROM NEW.size_bytes
       OR OLD.created_at IS DISTINCT FROM NEW.created_at THEN
        RAISE EXCEPTION ''incident evidence identity and content metadata are immutable'';
    END IF;
    RETURN NEW;
END';
DROP TRIGGER IF EXISTS incident_evidence_metadata_guard ON incident_evidence;
CREATE TRIGGER incident_evidence_metadata_guard
BEFORE UPDATE OR DELETE ON incident_evidence
FOR EACH ROW EXECUTE FUNCTION guard_incident_evidence_metadata();
