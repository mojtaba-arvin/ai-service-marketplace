-- TRUST-002 reviewer workflow: explicit reviewer grants and auditable forward-only transitions.
-- This migration extends the operator action allowlist without granting access by role alone.

ALTER TABLE operator_action_grants
    DROP CONSTRAINT IF EXISTS operator_action_grants_action_check;
ALTER TABLE operator_action_grants
    ADD CONSTRAINT operator_action_grants_action_check
    CHECK (action IN (
        'order:read','order:assign','provider:verify','case:read',
        'market:configure','catalog:manage','policy:manage','analytics:read',
        'operator:permissions:manage','incident:review'
    ));

ALTER TABLE operator_permission_events
    DROP CONSTRAINT IF EXISTS operator_permission_events_action_check;
ALTER TABLE operator_permission_events
    ADD CONSTRAINT operator_permission_events_action_check
    CHECK (action IN (
        'order:read','order:assign','provider:verify','case:read',
        'market:configure','catalog:manage','policy:manage','analytics:read',
        'operator:permissions:manage','incident:review'
    ));

ALTER TABLE incident_reports
    ADD COLUMN IF NOT EXISTS assigned_reviewer_identity_id text
        REFERENCES phone_identities(id) ON DELETE RESTRICT;

ALTER TABLE incident_report_events
    ADD COLUMN IF NOT EXISTS transaction_id bigint NOT NULL DEFAULT txid_current();

CREATE INDEX IF NOT EXISTS incident_reports_reviewer_queue_idx
    ON incident_reports(tenant_id,status,created_at DESC,id DESC);
CREATE INDEX IF NOT EXISTS incident_reports_assigned_reviewer_idx
    ON incident_reports(tenant_id,assigned_reviewer_identity_id,status,created_at DESC);
CREATE INDEX IF NOT EXISTS incident_report_events_transaction_idx
    ON incident_report_events(report_id,tenant_id,transaction_id,id);

-- A report row may change only when a matching append-only audit event was
-- inserted earlier in the same transaction. Reporter/order/content/evidence
-- fields remain immutable; DELETE is never allowed.
CREATE OR REPLACE FUNCTION guard_incident_report_identity()
RETURNS trigger LANGUAGE plpgsql
AS 'DECLARE
    audit_action text;
    status_changed boolean;
    severity_changed boolean;
    reviewer_changed boolean;
BEGIN
    IF TG_OP = ''DELETE'' THEN
        RAISE EXCEPTION ''incident reports cannot be deleted'';
    END IF;

    IF OLD.id IS DISTINCT FROM NEW.id
       OR OLD.order_id IS DISTINCT FROM NEW.order_id
       OR OLD.tenant_id IS DISTINCT FROM NEW.tenant_id
       OR OLD.reporter_identity_id IS DISTINCT FROM NEW.reporter_identity_id
       OR OLD.reporter_role IS DISTINCT FROM NEW.reporter_role
       OR OLD.category IS DISTINCT FROM NEW.category
       OR OLD.narrative IS DISTINCT FROM NEW.narrative
       OR OLD.occurred_at IS DISTINCT FROM NEW.occurred_at
       OR OLD.evidence_refs IS DISTINCT FROM NEW.evidence_refs
       OR OLD.created_at IS DISTINCT FROM NEW.created_at THEN
        RAISE EXCEPTION ''incident report identity, narrative and evidence fields are immutable'';
    END IF;

    IF OLD.status = ''closed'' THEN
        RAISE EXCEPTION ''closed incident reports cannot be changed'';
    END IF;

    status_changed := OLD.status IS DISTINCT FROM NEW.status;
    severity_changed := OLD.severity_hint IS DISTINCT FROM NEW.severity_hint;
    reviewer_changed := OLD.assigned_reviewer_identity_id IS DISTINCT FROM NEW.assigned_reviewer_identity_id;

    IF (CASE WHEN status_changed THEN 1 ELSE 0 END
        + CASE WHEN severity_changed THEN 1 ELSE 0 END
        + CASE WHEN reviewer_changed THEN 1 ELSE 0 END) <> 1 THEN
        RAISE EXCEPTION ''one audited incident report field may change per transaction'';
    END IF;

    IF status_changed AND NOT (
        (OLD.status = ''submitted'' AND NEW.status = ''triaged'')
        OR (OLD.status = ''triaged'' AND NEW.status = ''investigating'')
        OR (OLD.status = ''investigating'' AND NEW.status = ''resolved'')
        OR (OLD.status = ''resolved'' AND NEW.status = ''closed'')
    ) THEN
        RAISE EXCEPTION ''invalid incident report status transition'';
    END IF;

    IF severity_changed AND NEW.severity_hint NOT IN (''unknown'',''low'',''high'',''critical'') THEN
        RAISE EXCEPTION ''invalid incident report severity'';
    END IF;

    IF reviewer_changed AND NEW.assigned_reviewer_identity_id IS NOT NULL
       AND NOT EXISTS (
           SELECT 1
           FROM identity_memberships m
           JOIN operator_action_grants g
             ON g.identity_id=m.identity_id
            AND g.tenant_id=m.tenant_id
            AND g.action=''incident:review''
            AND g.revoked_at IS NULL
           WHERE m.identity_id=NEW.assigned_reviewer_identity_id
             AND m.tenant_id=NEW.tenant_id
             AND m.role=''operator''
       ) THEN
        RAISE EXCEPTION ''assigned identity is not an active same-tenant incident reviewer'';
    END IF;

    SELECT e.action INTO audit_action
    FROM incident_report_events e
    WHERE e.report_id=OLD.id
      AND e.tenant_id=OLD.tenant_id
      AND e.transaction_id=txid_current()
      AND e.before_state->>''status''=OLD.status
      AND e.before_state->>''severity_hint''=OLD.severity_hint
      AND e.before_state->>''assigned_reviewer_identity_id'' IS NOT DISTINCT FROM OLD.assigned_reviewer_identity_id
      AND e.after_state->>''status''=NEW.status
      AND e.after_state->>''severity_hint''=NEW.severity_hint
      AND e.after_state->>''assigned_reviewer_identity_id'' IS NOT DISTINCT FROM NEW.assigned_reviewer_identity_id
    ORDER BY e.id DESC
    LIMIT 1;

    IF audit_action IS NULL THEN
        RAISE EXCEPTION ''incident report update requires a matching same-transaction audit event'';
    END IF;
    IF status_changed AND audit_action <> ''status_changed'' THEN
        RAISE EXCEPTION ''status transition requires status_changed audit action'';
    END IF;
    IF severity_changed AND audit_action <> ''severity_changed'' THEN
        RAISE EXCEPTION ''severity update requires severity_changed audit action'';
    END IF;
    IF reviewer_changed AND audit_action <> ''assigned'' THEN
        RAISE EXCEPTION ''reviewer assignment requires assigned audit action'';
    END IF;

    NEW.updated_at := now();
    RETURN NEW;
END';
