CREATE TABLE IF NOT EXISTS auth_sessions (
    id text PRIMARY KEY,
    family_id text NOT NULL,
    phone_identity_id text NOT NULL REFERENCES phone_identities(id),
    access_token_hash bytea NOT NULL UNIQUE CHECK (octet_length(access_token_hash) = 32),
    refresh_token_hash bytea NOT NULL UNIQUE CHECK (octet_length(refresh_token_hash) = 32),
    access_expires_at timestamptz NOT NULL,
    refresh_expires_at timestamptz NOT NULL,
    revoked_at timestamptz,
    replaced_by text REFERENCES auth_sessions(id),
    created_at timestamptz NOT NULL DEFAULT now(),
    CHECK (refresh_expires_at > access_expires_at)
);
CREATE INDEX IF NOT EXISTS auth_sessions_family_idx ON auth_sessions(family_id);
CREATE INDEX IF NOT EXISTS auth_sessions_identity_idx ON auth_sessions(phone_identity_id);
