-- ADMIN-001: ensure every permission audit event describes its referenced grant.
-- A grant ID alone is insufficient: tenant, identity and action are part of its
-- security identity and must be enforced by PostgreSQL, not application convention.
-- This migration is idempotent because legacy installations may have schema
-- objects but no schema_migrations history when the runner first starts.
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conname='operator_action_grants_event_identity_unique'
          AND conrelid='operator_action_grants'::regclass
    ) THEN
        ALTER TABLE operator_action_grants
            ADD CONSTRAINT operator_action_grants_event_identity_unique
            UNIQUE (id, tenant_id, identity_id, action);
    END IF;

    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conname='operator_permission_events_grant_tuple_fk'
          AND conrelid='operator_permission_events'::regclass
    ) THEN
        ALTER TABLE operator_permission_events
            ADD CONSTRAINT operator_permission_events_grant_tuple_fk
            FOREIGN KEY (grant_id, tenant_id, identity_id, action)
            REFERENCES operator_action_grants (id, tenant_id, identity_id, action)
            ON UPDATE RESTRICT
            ON DELETE RESTRICT;
    END IF;
END $$;
