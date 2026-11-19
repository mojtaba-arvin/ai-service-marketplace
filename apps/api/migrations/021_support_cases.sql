CREATE TABLE IF NOT EXISTS support_agents (
    tenant_id text NOT NULL,
    identity_id text NOT NULL REFERENCES phone_identities(id) ON DELETE RESTRICT,
    active boolean NOT NULL DEFAULT true,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY(tenant_id,identity_id)
);

CREATE TABLE IF NOT EXISTS support_cases (
    id text PRIMARY KEY,
    tenant_id text NOT NULL,
    requester_identity_id text NOT NULL REFERENCES phone_identities(id) ON DELETE RESTRICT,
    category text NOT NULL CHECK (length(btrim(category)) BETWEEN 2 AND 80),
    priority text NOT NULL CHECK (priority IN ('low','normal','high','urgent')),
    status text NOT NULL CHECK (status IN ('open','in_progress','waiting_customer','resolved','closed')),
    title text NOT NULL CHECK (length(btrim(title)) BETWEEN 4 AND 180),
    description text NOT NULL CHECK (length(description)<=4000),
    assigned_support_identity_id text,
    sla_due_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    closed_at timestamptz,
    FOREIGN KEY(tenant_id,assigned_support_identity_id) REFERENCES support_agents(tenant_id,identity_id) ON DELETE RESTRICT,
    CHECK ((status='closed')=(closed_at IS NOT NULL)),
    CHECK (sla_due_at>created_at)
);
CREATE INDEX IF NOT EXISTS support_cases_tenant_status_idx ON support_cases(tenant_id,status,updated_at DESC);
CREATE INDEX IF NOT EXISTS support_cases_requester_idx ON support_cases(tenant_id,requester_identity_id,created_at DESC);
CREATE INDEX IF NOT EXISTS support_cases_sla_idx ON support_cases(sla_due_at) WHERE status NOT IN ('resolved','closed');

CREATE TABLE IF NOT EXISTS support_case_events (
    id bigserial PRIMARY KEY,
    case_id text NOT NULL REFERENCES support_cases(id) ON DELETE RESTRICT,
    actor_identity_id text NOT NULL REFERENCES phone_identities(id) ON DELETE RESTRICT,
    action text NOT NULL CHECK (action IN ('created','assigned','status_changed')),
    reason text NOT NULL,
    before_state jsonb NOT NULL,
    after_state jsonb NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS support_case_events_case_idx ON support_case_events(case_id,id);

CREATE OR REPLACE FUNCTION reject_support_case_event_mutation() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'support case events are append-only';
END $$;
DROP TRIGGER IF EXISTS support_case_events_immutable ON support_case_events;
CREATE TRIGGER support_case_events_immutable BEFORE UPDATE OR DELETE ON support_case_events
FOR EACH ROW EXECUTE FUNCTION reject_support_case_event_mutation();
