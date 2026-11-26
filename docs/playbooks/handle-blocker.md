# Handle a Blocker

## When
A specific requirement is ambiguous, a security/financial decision is missing, verification repeatedly fails, or external access is unavailable.

## Steps
1. Read `docs/playbooks/agent-execution.md` and classify the affected objective, not the entire command.
2. Distinguish:
   - `WAITING_DEPENDENCY`: a named internal prerequisite, shared integration, or upstream evidence is not ready.
   - `BLOCKED`: an authoritative human, legal, provider, privacy, safety, financial, permission, or policy decision is required.
   - `FAIL`: an attempted implementation or check produced a reproducible failure.
3. Stop only the work that depends on the missing decision/artifact. Check existing ADRs, specs, merged PRs, public module interfaces, and current main first.
4. For a shared contract owned by CMD-1754, submit a complete proposal and continue module-private implementation/tests that remain safe. Do not edit the protected shared path and do not mark the entire command blocked.
5. For a genuine human decision, create/update a GitHub issue with the authoritative source, exact question, options, impact, and recommended default. Never invent privacy, retention, legal, tax, safety, financial, or provider policy.
6. Continue every independent runnable objective. Keep the session in progress while runnable work remains.
7. Record the exact blocker/dependency, affected objective IDs, work that continued, files/SHAs, tests and next action in the tracking issue.

## Verify
Run `make verify` and, when database/Compose/outbox/E2E behavior changes, `make verify-full`. Report exact commands and outcomes. Never claim an external action succeeded without returned confirmation.
