# Handle a Blocker

## When
A specific requirement is ambiguous, a security/financial decision is missing, verification repeatedly fails, or external access is unavailable.

## Steps
1. Read `AGENTS.md` and `docs/playbooks/agent-execution.md`. Classify the affected objective, not the entire command.
2. Distinguish:
   - `WAITING_DEPENDENCY`: a named internal prerequisite, shared integration, or upstream evidence is not ready.
   - `BLOCKED`: an authoritative human, legal, provider, privacy, safety, financial, permission, or policy decision is required.
   - `FAIL`: an attempted implementation or check produced a reproducible failure.
   - `NOT-RUN`: work was not attempted; state why and whether it remains runnable.
3. Stop only work that depends on the missing decision or artifact. Check existing ADRs, specifications, merged PRs, public module interfaces, and current `main` first.
4. For a shared contract or integration owned by another designated repository owner, provide a concrete proposal with affected objectives, invariants, authorization/tenant rules, migration and rollback considerations, tests, compatibility impact, and exact files. Continue safe module-private implementation and tests; do not edit protected shared paths or label the entire command blocked.
5. For a genuine human decision, create or update a GitHub issue with the authoritative source, exact question, options, impact, and recommended default. Never invent privacy, retention, legal, tax, safety, financial, or provider policy.
6. Continue every independent runnable objective. Keep the session in progress while safe runnable work remains.
7. Record the affected objective IDs, exact disposition/dependency, work that continued, files/SHAs, tests and actual outcomes, and next action in the tracking issue.

## Verify
Run `make verify` and, when database/Compose/outbox/E2E behavior changes, `make verify-full`. Report exact commands and outcomes. Never claim an external action succeeded without returned confirmation.
