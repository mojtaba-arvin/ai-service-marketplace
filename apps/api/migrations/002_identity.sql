CREATE TABLE IF NOT EXISTS phone_identities (
    id text PRIMARY KEY,
    phone_e164 text NOT NULL UNIQUE CHECK (phone_e164 ~ '^\\+[1-9][0-9]{1,14}$'),
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS otp_challenges (
    id text PRIMARY KEY,
    phone_identity_id text NOT NULL REFERENCES phone_identities(id),
    code_hash bytea NOT NULL CHECK (octet_length(code_hash) = 32),
    expires_at timestamptz NOT NULL,
    attempt_count smallint NOT NULL DEFAULT 0 CHECK (attempt_count >= 0),
    max_attempts smallint NOT NULL CHECK (max_attempts BETWEEN 1 AND 10),
    consumed_at timestamptz,
    invalidated_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    CHECK (expires_at > created_at)
);
CREATE INDEX IF NOT EXISTS otp_challenges_identity_created_idx
    ON otp_challenges(phone_identity_id, created_at DESC);
CREATE INDEX IF NOT EXISTS otp_challenges_expiry_idx
    ON otp_challenges(expires_at) WHERE consumed_at IS NULL AND invalidated_at IS NULL;
