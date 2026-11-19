# Development Progress

> Update at the end of each engineering session. Do not mark features passing without independent QA evidence.

## Current phase
Foundation (0A), backlog decomposition (0B), and walking skeleton (0C) are implemented. Next: deliver product features M1–M14 in dependency order.

## Completed
- Repository harness, ADRs, agent instructions, playbooks, issue/PR templates, module scaffolds, Docker Compose, CI, and security workflows.
- 44 atomic backlog features across all 14 modules, with acceptance criteria, dependencies, S/M sizing, nature tags, and QA status; 14 module issues #5–#18.
- Backlog checker validates feature shape, unique IDs, dependency references/cycles, sizing, and QA evidence invariants.
- PostgreSQL walking skeleton: test-only OTP/auth, idempotent order creation, guarded state transitions, transactional outbox, idempotent outbox consumer, simulated balanced append-only ledger, and HTTP E2E.
- Session 0B issue #3 and Session 0C issue #4 are closed after their PR checks passed.

## Verification
- PR #1 CI and Security workflows passed on the latest foundation head.
- PR #19 CI passed on the latest backlog head; Security workflow is rerunning after aligning the dynamic language detector.
- PR #20 CI passed on the latest walking-skeleton head: Go formatting/tests, PostgreSQL integration, HTTP E2E, and Compose validation.
- PR #20 CodeQL Go/Python analysis is running on the latest head; do not merge until it passes.
- Dependency-review is skipped until the repository dependency graph is enabled and `DEPENDENCY_GRAPH_ENABLED=true` is set.
- No local checkout/test execution was available in this environment.

## Open owner decisions / repository settings
- Enable dependency graph and set `DEPENDENCY_GRAPH_ENABLED=true`.
- Configure main-branch ruleset, required checks, Actions permissions, Dependabot, secret scanning/push protection.
- Replace placeholder CODEOWNERS with verified maintainer/team handles.
- Choose a project license and confirm launch jurisdiction before enabling regulated payment/KYC/tax/labor workflows.

## Product backlog
- 44 atomic M1–M14 capabilities are tracked. Product work is proceeding in dependency order; `passes` remains false until independent QA evidence is recorded.
- Dispatch implementation is now present in three stacked PRs: #39 provider availability/calendar and conflict-safe booking; #40 deterministic candidate eligibility; #41 bounded expiring offers, decline/expiry, and atomic single-winner assignment.
- PR #39 and PR #40 have passing CI and Security workflows on their latest heads. PR #41 adds PostgreSQL concurrency, decline, expiry, and immutability tests; its latest CI/Security runs are pending at this update.
- Dispatch includes a separate graceful expiry worker, immutable offer/round audit records, transactional outbox events, and provider-slot reservation in the acceptance transaction.
- No independent QA sign-off has been recorded; do not set `passes: true` based solely on implementation PR checks.
- Next dependency-ordered work: expose dispatch application operations through the authenticated API boundary, then continue downstream order/professional workflows. Do not use the test-only OTP adapter as production authentication.
- Financial invariants remain mandatory: integer minor units, balanced immutable double-entry journals, idempotency, and compensating reversals.
- AI suggestions must never authorize identity/safety decisions or set payable prices.
