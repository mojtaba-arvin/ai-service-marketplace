CREATE TABLE IF NOT EXISTS order_service_form_answers (
    order_id text PRIMARY KEY REFERENCES orders(id) ON DELETE RESTRICT,
    form_version_id text NOT NULL REFERENCES service_form_versions(id) ON DELETE RESTRICT,
    answers jsonb NOT NULL CHECK (jsonb_typeof(answers) = 'object'),
    submitted_by text NOT NULL,
    submitted_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS order_service_form_answers_version_idx
    ON order_service_form_answers(form_version_id);

CREATE OR REPLACE FUNCTION guard_order_service_form_answers_mutation() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'order service form answer snapshots are immutable';
END $$;

DROP TRIGGER IF EXISTS order_service_form_answers_guard ON order_service_form_answers;
CREATE TRIGGER order_service_form_answers_guard
BEFORE UPDATE OR DELETE ON order_service_form_answers
FOR EACH ROW EXECUTE FUNCTION guard_order_service_form_answers_mutation();

CREATE OR REPLACE FUNCTION reject_order_service_form_answers_truncate() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'order service form answer history cannot be truncated';
END $$;

DROP TRIGGER IF EXISTS order_service_form_answers_no_truncate ON order_service_form_answers;
CREATE TRIGGER order_service_form_answers_no_truncate
BEFORE TRUNCATE ON order_service_form_answers
FOR EACH STATEMENT EXECUTE FUNCTION reject_order_service_form_answers_truncate();
