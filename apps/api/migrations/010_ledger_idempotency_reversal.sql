ALTER TABLE ledger_journals
    ADD COLUMN IF NOT EXISTS request_hash bytea,
    ADD COLUMN IF NOT EXISTS reversal_of_id text;

DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname='ledger_journals_request_hash_length') THEN
        ALTER TABLE ledger_journals ADD CONSTRAINT ledger_journals_request_hash_length
            CHECK (request_hash IS NULL OR octet_length(request_hash)=32);
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname='ledger_journals_reversal_fk') THEN
        ALTER TABLE ledger_journals ADD CONSTRAINT ledger_journals_reversal_fk
            FOREIGN KEY (reversal_of_id) REFERENCES ledger_journals(id) ON DELETE RESTRICT;
    END IF;
END $$;

CREATE UNIQUE INDEX IF NOT EXISTS ledger_journals_one_reversal_idx
    ON ledger_journals(reversal_of_id) WHERE reversal_of_id IS NOT NULL;

CREATE OR REPLACE FUNCTION reject_ledger_journal_mutation() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'posted ledger journals are immutable; post a compensating reversal';
END $$;
DROP TRIGGER IF EXISTS ledger_journals_immutable ON ledger_journals;
CREATE TRIGGER ledger_journals_immutable
BEFORE UPDATE OR DELETE ON ledger_journals
FOR EACH ROW EXECUTE FUNCTION reject_ledger_journal_mutation();
