CREATE TABLE IF NOT EXISTS market_config_audit (
    id bigserial PRIMARY KEY,
    market_code text NOT NULL REFERENCES markets(code) ON DELETE RESTRICT,
    actor_tenant_id text NOT NULL,
    actor_subject_id text NOT NULL,
    action text NOT NULL CHECK (action IN ('created', 'updated')),
    previous_version integer,
    new_version integer NOT NULL CHECK (new_version > 0),
    before_config jsonb,
    after_config jsonb NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    CHECK (
        (action = 'created' AND previous_version IS NULL AND before_config IS NULL)
        OR
        (action = 'updated' AND previous_version IS NOT NULL AND before_config IS NOT NULL)
    )
);

CREATE UNIQUE INDEX IF NOT EXISTS market_config_audit_version_idx
    ON market_config_audit (market_code, new_version);

CREATE OR REPLACE FUNCTION reject_market_config_audit_mutation()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    RAISE EXCEPTION 'market_config_audit is append-only';
END;
$$;

DROP TRIGGER IF EXISTS market_config_audit_no_update_delete ON market_config_audit;
CREATE TRIGGER market_config_audit_no_update_delete
    BEFORE UPDATE OR DELETE ON market_config_audit
    FOR EACH ROW EXECUTE FUNCTION reject_market_config_audit_mutation();

DROP TRIGGER IF EXISTS market_config_audit_no_truncate ON market_config_audit;
CREATE TRIGGER market_config_audit_no_truncate
    BEFORE TRUNCATE ON market_config_audit
    FOR EACH STATEMENT EXECUTE FUNCTION reject_market_config_audit_mutation();
