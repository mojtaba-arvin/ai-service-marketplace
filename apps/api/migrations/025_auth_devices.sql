CREATE TABLE IF NOT EXISTS auth_devices (
    id text PRIMARY KEY,
    identity_id text NOT NULL REFERENCES phone_identities(id) ON DELETE RESTRICT,
    label text NOT NULL CHECK (length(btrim(label)) BETWEEN 1 AND 80),
    created_at timestamptz NOT NULL DEFAULT now(),
    last_seen_at timestamptz NOT NULL DEFAULT now(),
    revoked_at timestamptz
);

ALTER TABLE auth_sessions
    ADD COLUMN IF NOT EXISTS device_id text REFERENCES auth_devices(id) ON DELETE SET NULL;

CREATE INDEX IF NOT EXISTS auth_devices_identity_created_idx
    ON auth_devices(identity_id, created_at DESC, id);
CREATE INDEX IF NOT EXISTS auth_sessions_device_active_idx
    ON auth_sessions(device_id) WHERE revoked_at IS NULL;
