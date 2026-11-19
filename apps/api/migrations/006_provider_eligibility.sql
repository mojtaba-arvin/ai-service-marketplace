CREATE TABLE IF NOT EXISTS provider_profiles (
    id text PRIMARY KEY,
    identity_id text NOT NULL UNIQUE REFERENCES phone_identities(id),
    tenant_id text NOT NULL,
    display_name text NOT NULL CHECK (length(btrim(display_name)) BETWEEN 1 AND 160),
    active boolean NOT NULL DEFAULT false,
    verified_at timestamptz,
    verified_by_identity_id text REFERENCES phone_identities(id),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CHECK ((verified_at IS NULL) = (verified_by_identity_id IS NULL))
);

CREATE TABLE IF NOT EXISTS provider_skills (
    id text PRIMARY KEY,
    provider_id text NOT NULL REFERENCES provider_profiles(id) ON DELETE RESTRICT,
    category_id text NOT NULL REFERENCES service_categories(id) ON DELETE RESTRICT,
    verified_at timestamptz,
    verified_by_identity_id text REFERENCES phone_identities(id),
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE(provider_id, category_id),
    CHECK ((verified_at IS NULL) = (verified_by_identity_id IS NULL))
);

CREATE TABLE IF NOT EXISTS provider_service_areas (
    id text PRIMARY KEY,
    provider_id text NOT NULL REFERENCES provider_profiles(id) ON DELETE RESTRICT,
    country_code char(2) NOT NULL CHECK (country_code ~ '^[A-Z]{2}$'),
    region_code text NOT NULL CHECK (length(btrim(region_code)) BETWEEN 1 AND 100),
    city_code text NOT NULL CHECK (length(btrim(city_code)) BETWEEN 1 AND 100),
    area_code text NOT NULL CHECK (length(btrim(area_code)) BETWEEN 1 AND 100),
    active boolean NOT NULL DEFAULT true,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE(provider_id,country_code,region_code,city_code,area_code)
);

CREATE INDEX IF NOT EXISTS provider_skills_match_idx
    ON provider_skills(category_id,provider_id) WHERE verified_at IS NOT NULL;
CREATE INDEX IF NOT EXISTS provider_areas_match_idx
    ON provider_service_areas(country_code,region_code,city_code,area_code,provider_id)
    WHERE active=true;
CREATE INDEX IF NOT EXISTS provider_profiles_eligible_idx
    ON provider_profiles(tenant_id,id) WHERE active=true AND verified_at IS NOT NULL;
