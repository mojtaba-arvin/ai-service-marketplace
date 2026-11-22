-- Tenant-scoped B2B organization and property registry foundation.
-- An organization is a business grouping, never a replacement for tenant isolation.

CREATE TABLE IF NOT EXISTS organizations (
    id text PRIMARY KEY,
    tenant_id text NOT NULL CHECK (length(btrim(tenant_id)) BETWEEN 1 AND 120),
    name text NOT NULL CHECK (length(btrim(name)) BETWEEN 1 AND 160),
    slug text NOT NULL CHECK (slug ~ '^[a-z0-9]+([a-z0-9-]*[a-z0-9])?$'),
    status text NOT NULL DEFAULT 'active' CHECK (status IN ('active','archived')),
    created_by_identity_id text NOT NULL REFERENCES phone_identities(id),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (id, tenant_id),
    UNIQUE (tenant_id, slug)
);

CREATE TABLE IF NOT EXISTS organization_memberships (
    id text PRIMARY KEY,
    organization_id text NOT NULL,
    tenant_id text NOT NULL,
    identity_id text NOT NULL,
    identity_role text NOT NULL CHECK (identity_role IN ('customer','professional','operator','support')),
    role text NOT NULL CHECK (role IN ('owner','admin','manager','member','viewer')),
    status text NOT NULL DEFAULT 'active' CHECK (status IN ('invited','active','revoked')),
    invited_by_identity_id text REFERENCES phone_identities(id),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    revoked_at timestamptz,
    UNIQUE (organization_id, tenant_id, identity_id, identity_role),
    FOREIGN KEY (organization_id, tenant_id)
        REFERENCES organizations(id, tenant_id) ON DELETE RESTRICT,
    FOREIGN KEY (identity_id, tenant_id, identity_role)
        REFERENCES identity_memberships(identity_id, tenant_id, role) ON DELETE RESTRICT,
    CHECK ((status = 'revoked') = (revoked_at IS NOT NULL))
);

CREATE INDEX IF NOT EXISTS organization_memberships_active_identity_idx
    ON organization_memberships(tenant_id, identity_id, organization_id)
    WHERE status = 'active';

CREATE TABLE IF NOT EXISTS properties (
    id text PRIMARY KEY,
    tenant_id text NOT NULL,
    organization_id text NOT NULL,
    property_code text NOT NULL CHECK (length(btrim(property_code)) BETWEEN 1 AND 80),
    name text NOT NULL CHECK (length(btrim(name)) BETWEEN 1 AND 160),
    country_code text NOT NULL CHECK (country_code ~ '^[A-Z]{2}$'),
    region_code text NOT NULL CHECK (length(btrim(region_code)) BETWEEN 1 AND 100),
    city_code text NOT NULL CHECK (length(btrim(city_code)) BETWEEN 1 AND 100),
    area_code text NOT NULL CHECK (length(btrim(area_code)) BETWEEN 1 AND 100),
    status text NOT NULL DEFAULT 'active' CHECK (status IN ('active','archived')),
    created_by_identity_id text NOT NULL REFERENCES phone_identities(id),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (id, tenant_id, organization_id),
    UNIQUE (organization_id, tenant_id, property_code),
    FOREIGN KEY (organization_id, tenant_id)
        REFERENCES organizations(id, tenant_id) ON DELETE RESTRICT
);

CREATE INDEX IF NOT EXISTS properties_tenant_org_status_idx
    ON properties(tenant_id, organization_id, status, created_at DESC);

CREATE TABLE IF NOT EXISTS property_memberships (
    id text PRIMARY KEY,
    property_id text NOT NULL,
    organization_id text NOT NULL,
    tenant_id text NOT NULL,
    identity_id text NOT NULL,
    identity_role text NOT NULL CHECK (identity_role IN ('customer','professional','operator','support')),
    role text NOT NULL CHECK (role IN ('manager','viewer')),
    status text NOT NULL DEFAULT 'active' CHECK (status IN ('active','revoked')),
    created_by_identity_id text NOT NULL REFERENCES phone_identities(id),
    created_at timestamptz NOT NULL DEFAULT now(),
    revoked_at timestamptz,
    UNIQUE (property_id, tenant_id, identity_id, identity_role),
    FOREIGN KEY (property_id, tenant_id, organization_id)
        REFERENCES properties(id, tenant_id, organization_id) ON DELETE RESTRICT,
    FOREIGN KEY (organization_id, tenant_id, identity_id, identity_role)
        REFERENCES organization_memberships(organization_id, tenant_id, identity_id, identity_role) ON DELETE RESTRICT,
    CHECK ((status = 'revoked') = (revoked_at IS NOT NULL))
);

CREATE INDEX IF NOT EXISTS property_memberships_active_identity_idx
    ON property_memberships(tenant_id, identity_id, property_id)
    WHERE status = 'active';

CREATE TABLE IF NOT EXISTS organization_audit_events (
    id text PRIMARY KEY,
    organization_id text NOT NULL,
    tenant_id text NOT NULL,
    actor_identity_id text NOT NULL REFERENCES phone_identities(id),
    action text NOT NULL CHECK (length(btrim(action)) BETWEEN 1 AND 100),
    details jsonb NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(details) = 'object'),
    created_at timestamptz NOT NULL DEFAULT now(),
    FOREIGN KEY (organization_id, tenant_id)
        REFERENCES organizations(id, tenant_id) ON DELETE RESTRICT
);

CREATE INDEX IF NOT EXISTS organization_audit_events_scope_idx
    ON organization_audit_events(tenant_id, organization_id, created_at DESC);

CREATE OR REPLACE FUNCTION reject_organization_audit_mutation() RETURNS trigger AS $$
BEGIN
    RAISE EXCEPTION 'organization audit events are append-only';
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS organization_audit_events_immutable ON organization_audit_events;
CREATE TRIGGER organization_audit_events_immutable
    BEFORE UPDATE OR DELETE ON organization_audit_events
    FOR EACH ROW EXECUTE FUNCTION reject_organization_audit_mutation();
