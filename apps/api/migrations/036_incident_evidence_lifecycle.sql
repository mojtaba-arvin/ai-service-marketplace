-- TRUST-002: separate evidence lifecycle authority and reasoned audit snapshots.
-- No retention defaults or byte deletion are introduced by this migration.

ALTER TABLE operator_action_grants
    DROP CONSTRAINT IF EXISTS operator_action_grants_action_check;
ALTER TABLE operator_action_grants
    ADD CONSTRAINT operator_action_grants_action_check
    CHECK (action IN (
        'order:read','order:assign','provider:verify','case:read',
        'market:configure','catalog:manage','policy:manage','analytics:read',
        'operator:permissions:manage','incident:review','incident:evidence:manage'
    ));

ALTER TABLE operator_permission_events
    DROP CONSTRAINT IF EXISTS operator_permission_events_action_check;
ALTER TABLE operator_permission_events
    ADD CONSTRAINT operator_permission_events_action_check
    CHECK (action IN (
        'order:read','order:assign','provider:verify','case:read',
        'market:configure','catalog:manage','policy:manage','analytics:read',
        'operator:permissions:manage','incident:review','incident:evidence:manage'
    ));

ALTER TABLE incident_evidence_access_events
    ADD COLUMN IF NOT EXISTS reason text NOT NULL DEFAULT ''
        CHECK (length(reason) <= 500),
    ADD COLUMN IF NOT EXISTS before_state jsonb NOT NULL DEFAULT '{}'::jsonb,
    ADD COLUMN IF NOT EXISTS after_state jsonb NOT NULL DEFAULT '{}'::jsonb;

ALTER TABLE incident_evidence_access_events
    DROP CONSTRAINT IF EXISTS incident_evidence_access_events_action_check;
ALTER TABLE incident_evidence_access_events
    ADD CONSTRAINT incident_evidence_access_events_action_check
    CHECK (action IN (
        'upload','download','list',
        'retention_update','legal_hold_set','legal_hold_cleared'
    ));

ALTER TABLE incident_evidence_access_events
    DROP CONSTRAINT IF EXISTS incident_evidence_access_events_reason_category_check;
ALTER TABLE incident_evidence_access_events
    ADD CONSTRAINT incident_evidence_access_events_reason_category_check
    CHECK (reason_category IN (
        'accepted','unauthorized','not_found','invalid_content','too_large',
        'limit_reached','scanner_unavailable','scanner_rejected','storage_failure',
        'key_failure','retention_expired','audit_failure',
        'operator_requested','invalid_input','state_conflict'
    ));
