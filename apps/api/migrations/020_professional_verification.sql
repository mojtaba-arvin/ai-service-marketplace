CREATE TABLE IF NOT EXISTS professional_verification_cases (
    id text PRIMARY KEY,
    provider_id text NOT NULL REFERENCES provider_profiles(id) ON DELETE RESTRICT,
    subject_identity_id text NOT NULL REFERENCES phone_identities(id) ON DELETE RESTRICT,
    status text NOT NULL CHECK (status IN ('submitted','in_review','approved','rejected')),
    evidence_ref text NOT NULL CHECK (length(btrim(evidence_ref)) BETWEEN 12 AND 512),
    reviewer_identity_id text REFERENCES phone_identities(id) ON DELETE RESTRICT,
    decision_reason text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    decided_at timestamptz,
    CHECK ((status IN ('submitted','in_review') AND reviewer_identity_id IS NULL AND decided_at IS NULL)
        OR (status IN ('approved','rejected') AND reviewer_identity_id IS NOT NULL AND decided_at IS NOT NULL))
);
CREATE UNIQUE INDEX IF NOT EXISTS professional_verification_one_open_case
    ON professional_verification_cases(provider_id)
    WHERE status IN ('submitted','in_review');
CREATE INDEX IF NOT EXISTS professional_verification_tenant_status_idx
    ON professional_verification_cases(status,updated_at);

CREATE TABLE IF NOT EXISTS professional_verification_events (
    id bigserial PRIMARY KEY,
    case_id text NOT NULL REFERENCES professional_verification_cases(id) ON DELETE RESTRICT,
    actor_identity_id text NOT NULL REFERENCES phone_identities(id) ON DELETE RESTRICT,
    action text NOT NULL CHECK (action IN ('submitted','reviewed')),
    from_status text,
    to_status text NOT NULL,
    reason text NOT NULL DEFAULT '',
    evidence_ref text,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS professional_verification_events_case_idx
    ON professional_verification_events(case_id,id);

CREATE OR REPLACE FUNCTION reject_professional_verification_event_mutation() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'professional verification events are append-only';
END $$;
DROP TRIGGER IF EXISTS professional_verification_events_immutable ON professional_verification_events;
CREATE TRIGGER professional_verification_events_immutable
BEFORE UPDATE OR DELETE ON professional_verification_events
FOR EACH ROW EXECUTE FUNCTION reject_professional_verification_event_mutation();
