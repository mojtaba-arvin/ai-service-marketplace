CREATE TABLE IF NOT EXISTS order_completion_approvals (
    order_id text PRIMARY KEY REFERENCES orders(id) ON DELETE RESTRICT,
    customer_identity_id text NOT NULL REFERENCES phone_identities(id) ON DELETE RESTRICT,
    approved_at timestamptz NOT NULL DEFAULT now()
);

CREATE OR REPLACE FUNCTION guard_order_completion_approval_mutation() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'order completion approvals are immutable';
END $$;

DROP TRIGGER IF EXISTS order_completion_approvals_guard ON order_completion_approvals;
CREATE TRIGGER order_completion_approvals_guard
BEFORE UPDATE OR DELETE ON order_completion_approvals
FOR EACH ROW EXECUTE FUNCTION guard_order_completion_approval_mutation();

CREATE OR REPLACE FUNCTION reject_order_completion_approvals_truncate() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'order completion approval history cannot be truncated';
END $$;

DROP TRIGGER IF EXISTS order_completion_approvals_no_truncate ON order_completion_approvals;
CREATE TRIGGER order_completion_approvals_no_truncate
BEFORE TRUNCATE ON order_completion_approvals
FOR EACH STATEMENT EXECUTE FUNCTION reject_order_completion_approvals_truncate();
