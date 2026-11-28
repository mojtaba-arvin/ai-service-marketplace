-- Retryable private-evidence deletion. Rows and audit events remain permanent tombstones.
ALTER TABLE incident_evidence
 ADD COLUMN IF NOT EXISTS deletion_state text NOT NULL DEFAULT 'active' CHECK(deletion_state IN ('active','deletion_pending','deleted')),
 ADD COLUMN IF NOT EXISTS deletion_attempts integer NOT NULL DEFAULT 0 CHECK(deletion_attempts>=0),
 ADD COLUMN IF NOT EXISTS deletion_claim_token text,
 ADD COLUMN IF NOT EXISTS deletion_claimed_until timestamptz,
 ADD COLUMN IF NOT EXISTS deleted_at timestamptz,
 ADD COLUMN IF NOT EXISTS deletion_error_category text NOT NULL DEFAULT '' CHECK(deletion_error_category IN ('','storage_failure'));
ALTER TABLE incident_evidence DROP CONSTRAINT IF EXISTS incident_evidence_deletion_state_consistency;
ALTER TABLE incident_evidence ADD CONSTRAINT incident_evidence_deletion_state_consistency CHECK (
 (deletion_state='active' AND deletion_claim_token IS NULL AND deletion_claimed_until IS NULL AND deleted_at IS NULL)
 OR (deletion_state='deletion_pending' AND deleted_at IS NULL)
 OR (deletion_state='deleted' AND deletion_claim_token IS NULL AND deletion_claimed_until IS NULL AND deleted_at IS NOT NULL));
CREATE INDEX IF NOT EXISTS incident_evidence_deletion_candidates_idx ON incident_evidence(retention_expires_at,created_at,id)
 WHERE legal_hold=false AND deletion_state IN ('active','deletion_pending');
ALTER TABLE incident_evidence_access_events DROP CONSTRAINT IF EXISTS incident_evidence_access_events_action_check;
ALTER TABLE incident_evidence_access_events ADD CONSTRAINT incident_evidence_access_events_action_check CHECK(action IN (
 'upload','download','list','retention_update','legal_hold_set','legal_hold_cleared',
 'deletion_claimed','deletion_succeeded','deletion_failed','deletion_reconciled'));
ALTER TABLE incident_evidence_access_events DROP CONSTRAINT IF EXISTS incident_evidence_access_events_outcome_check;
ALTER TABLE incident_evidence_access_events ADD CONSTRAINT incident_evidence_access_events_outcome_check CHECK(outcome IN ('success','denied','failure'));
ALTER TABLE incident_evidence_access_events DROP CONSTRAINT IF EXISTS incident_evidence_access_events_reason_category_check;
ALTER TABLE incident_evidence_access_events ADD CONSTRAINT incident_evidence_access_events_reason_category_check CHECK(reason_category IN (
 'accepted','unauthorized','not_found','invalid_content','too_large','limit_reached','scanner_unavailable','scanner_rejected',
 'storage_failure','key_failure','retention_expired','audit_failure','operator_requested','invalid_input','state_conflict',
 'missing_object','deletion_retry','reconciliation'));
CREATE OR REPLACE FUNCTION guard_incident_evidence_metadata()
RETURNS trigger LANGUAGE plpgsql AS 'BEGIN
 IF TG_OP = ''DELETE'' THEN RAISE EXCEPTION ''incident evidence metadata is a permanent tombstone''; END IF;
 IF OLD.id IS DISTINCT FROM NEW.id OR OLD.report_id IS DISTINCT FROM NEW.report_id
 OR OLD.tenant_id IS DISTINCT FROM NEW.tenant_id OR OLD.reporter_identity_id IS DISTINCT FROM NEW.reporter_identity_id
 OR OLD.media_type IS DISTINCT FROM NEW.media_type OR OLD.size_bytes IS DISTINCT FROM NEW.size_bytes
 OR OLD.created_at IS DISTINCT FROM NEW.created_at THEN
 RAISE EXCEPTION ''incident evidence identity and content metadata are immutable''; END IF;
 RETURN NEW; END';
