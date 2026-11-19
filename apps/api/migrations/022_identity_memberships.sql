CREATE TABLE IF NOT EXISTS identity_memberships (
    id text PRIMARY KEY,
    identity_id text NOT NULL REFERENCES phone_identities(id) ON DELETE CASCADE,
    tenant_id text NOT NULL CHECK (length(btrim(tenant_id)) BETWEEN 1 AND 120),
    role text NOT NULL CHECK (role IN ('customer','professional','operator','support')),
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE(identity_id,tenant_id,role)
);

CREATE INDEX IF NOT EXISTS identity_memberships_tenant_role_idx
    ON identity_memberships(tenant_id,role,identity_id);
