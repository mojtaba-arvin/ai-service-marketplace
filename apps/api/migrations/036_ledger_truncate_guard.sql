-- PostgreSQL row-level UPDATE/DELETE triggers do not fire for TRUNCATE.
-- Reject statement-level truncation so journal history cannot be erased through
-- a bulk operation that bypasses the existing append-only row triggers.
CREATE OR REPLACE FUNCTION reject_ledger_truncate() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'ledger history is append-only; TRUNCATE is forbidden';
END $$;

DROP TRIGGER IF EXISTS ledger_entries_no_truncate ON ledger_entries;
CREATE TRIGGER ledger_entries_no_truncate
BEFORE TRUNCATE ON ledger_entries
FOR EACH STATEMENT EXECUTE FUNCTION reject_ledger_truncate();

DROP TRIGGER IF EXISTS ledger_journals_no_truncate ON ledger_journals;
CREATE TRIGGER ledger_journals_no_truncate
BEFORE TRUNCATE ON ledger_journals
FOR EACH STATEMENT EXECUTE FUNCTION reject_ledger_truncate();
