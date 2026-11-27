-- ADMIN-001: scoped operator grants and immutable provisioning evidence.
-- Existing operator memberships intentionally receive no grants.
CREATE TABLE IF NOT EXISTS operator_permission_bootstraps (
    tenant_id text PRIMARY KEY CHECK (length(btrim(tenant_id)) BETWEEN 1 AND 120),
    bootstrap_identity_id text NOT NULL REFERENCES phone_identities(id) ON DELETE RESTRICT,
    manifest_sha256 char(64) NOT NULL CHECK (manifest_sha256 ~ '^[0-9a-f]{64}$'),
    grants_count integer NOT NULL CHECK (grants_count > 0),
    created_by text NOT NULL CHECK (created_by = 'system_bootstrap'),
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS operator_action_grants (
    id text PRIMARY KEY,
    identity_id text NOT NULL REFERENCES phone_identities(id) ON DELETE RESTRICT,
    tenant_id text NOT NULL CHECK (length(btrim(tenant_id)) BETWEEN 1 AND 120),
    action text NOT NULL CHECK (action IN (
        'order:read',
        'order:assign',
        'provider:verify',
        'case:read',
        'market:configure',
        'catalog:manage',
        'policy:manage',
        'analytics:read',
        'operator:permissions:manage'
    )),
    granted_by_kind text NOT NULL CHECK (granted_by_kind IN ('system_bootstrap','operator')),
    granted_by_identity_id text REFERENCES phone_identities(id) ON DELETE RESTRICT,
    grant_reason text NOT NULL CHECK (length(btrim(grant_reason)) BETWEEN 1 AND 500),
    granted_at timestamptz NOT NULL DEFAULT now(),
    revoked_at timestamptz,
    revoked_by_identity_id text REFERENCES phone_identities(id) ON DELETE RESTRICT,
    revoke_reason text NOT NULL DEFAULT '',
    CHECK (
        (granted_by_kind='system_bootstrap' AND granted_by_identity_id IS NULL)
        OR (granted_by_kind='operator' AND granted_by_identity_id IS NOT NULL)
    ),
    CHECK (
        (revoked_at IS NULL AND revoked_by_identity_id IS NULL AND revoke_reason='')
        OR (revoked_at IS NOT NULL AND revoked_by_identity_id IS NOT NULL AND length(btrim(revoke_reason)) BETWEEN 1 AND 500)
    )
);

CREATE UNIQUE INDEX IF NOT EXISTS operator_action_grants_active_unique
    ON operator_action_grants(identity_id,tenant_id,action)
    WHERE revoked_at IS NULL;
CREATE INDEX IF NOT EXISTS operator_action_grants_lookup_idx
    ON operator_action_grants(tenant_id,identity_id,action)
    WHERE revoked_at IS NULL;

CREATE TABLE IF NOT EXISTS operator_permission_events (
    id bigserial PRIMARY KEY,
    grant_id text NOT NULL REFERENCES operator_action_grants(id) ON DELETE RESTRICT,
    tenant_id text NOT NULL CHECK (length(btrim(tenant_id)) BETWEEN 1 AND 120),
    identity_id text NOT NULL REFERENCES phone_identities(id) ON DELETE RESTRICT,
    action text NOT NULL CHECK (action IN (
        'order:read',
        'order:assign',
        'provider:verify',
        'case:read',
        'market:configure',
        'catalog:manage',
        'policy:manage',
        'analytics:read',
        'operator:permissions:manage'
    )),
    event_type text NOT NULL CHECK (event_type IN ('granted','revoked')),
    actor_kind text NOT NULL CHECK (actor_kind IN ('system_bootstrap','operator')),
    actor_identity_id text REFERENCES phone_identities(id) ON DELETE RESTRICT,
    reason text NOT NULL CHECK (length(btrim(reason)) BETWEEN 1 AND 500),
    before_state jsonb NOT NULL DEFAULT '{}'::jsonb,
    after_state jsonb NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    CHECK (
        (actor_kind='system_bootstrap' AND actor_identity_id IS NULL)
        OR (actor_kind='operator' AND actor_identity_id IS NOT NULL)
    )
);
CREATE INDEX IF NOT EXISTS operator_permission_events_tenant_idx
    ON operator_permission_events(tenant_id,created_at,id);

CREATE OR REPLACE FUNCTION reject_operator_permission_event_mutation() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'operator permission events are append-only';
END $$;
DROP TRIGGER IF EXISTS operator_permission_events_immutable ON operator_permission_events;
CREATE TRIGGER operator_permission_events_immutable
BEFORE UPDATE OR DELETE ON operator_permission_events
FOR EACH ROW EXECUTE FUNCTION reject_operator_permission_event_mutation();

CREATE OR REPLACE FUNCTION guard_operator_permission_grant_mutation() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP='DELETE' THEN
        RAISE EXCEPTION 'operator grants cannot be deleted; revoke them';
    END IF;
    IF OLD.id IS DISTINCT FROM NEW.id
       OR OLD.identity_id IS DISTINCT FROM NEW.identity_id
       OR OLD.tenant_id IS DISTINCT FROM NEW.tenant_id
       OR OLD.action IS DISTINCT FROM NEW.action
       OR OLD.granted_by_kind IS DISTINCT FROM NEW.granted_by_kind
       OR OLD.granted_by_identity_id IS DISTINCT FROM NEW.granted_by_identity_id
       OR OLD.grant_reason IS DISTINCT FROM NEW.grant_reason
       OR OLD.granted_at IS DISTINCT FROM NEW.granted_at
       OR OLD.revoked_at IS NOT NULL
       OR NEW.revoked_at IS NULL
       OR NEW.revoked_by_identity_id IS NULL
       OR length(btrim(NEW.revoke_reason)) NOT BETWEEN 1 AND 500 THEN
        RAISE EXCEPTION 'operator grants are immutable except for one-way revocation';
    END IF;
    RETURN NEW;
END $$;
DROP TRIGGER IF EXISTS operator_action_grants_guard ON operator_action_grants;
CREATE TRIGGER operator_action_grants_guard
BEFORE UPDATE OR DELETE ON operator_action_grants
FOR EACH ROW EXECUTE FUNCTION guard_operator_permission_grant_mutation();

CREATE OR REPLACE FUNCTION reject_operator_permission_bootstrap_mutation() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'operator permission bootstrap markers are immutable';
END $$;
DROP TRIGGER IF EXISTS operator_permission_bootstraps_immutable ON operator_permission_bootstraps;
CREATE TRIGGER operator_permission_bootstraps_immutable
BEFORE UPDATE OR DELETE ON operator_permission_bootstraps
FOR EACH ROW EXECUTE FUNCTION reject_operator_permission_bootstrap_mutation();
