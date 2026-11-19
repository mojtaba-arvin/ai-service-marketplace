# Add a Database Migration

## When
A schema change is required.

## Steps
1. Identify the owning module and data owner.
2. Prefer additive, backward-compatible expand/contract migrations.
3. Define indexes and constraints from query and integrity requirements.
4. Estimate lock and table-rewrite risk for production-sized data.
5. Include rollback/recovery notes; never assume destructive rollback is safe.
6. Update schema contracts and integration tests.
7. Require human review for destructive, data-loss, or financial-ledger migrations.

## Verify
Run migration tests against the supported PostgreSQL version and `make verify-full`. Record execution and rollback considerations in the PR.
