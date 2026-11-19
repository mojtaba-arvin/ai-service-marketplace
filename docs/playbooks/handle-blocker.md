# Handle a Blocker

## When
A requirement is ambiguous, a security/financial decision is missing, verification repeatedly fails, or external access is unavailable.

## Steps
1. Stop work that depends on the unresolved decision.
2. Reproduce the blocker and record exact command/output or missing permission.
3. Check whether an existing ADR or spec already answers it.
4. If not, create/update a GitHub issue with `needs-human`, a concise decision question, options, impact, and a recommended default.
5. Update `docs/progress.md` with the blocker and next safe task.
6. Continue only with independent, non-blocked work.

## Verify
Run `make verify` and ensure the blocker is explicit in the issue/PR. Never claim an external action succeeded without a returned confirmation.
