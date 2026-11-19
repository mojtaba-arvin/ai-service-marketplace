# Development Progress

> Keep this file concise (maximum 60 lines). Update it at the end of each work session.

## Current phase
Session 0B complete; Session 0C walking skeleton is in implementation and not yet verified.

## Completed
- Established repository foundation, agent guidance, ADRs, module placeholders, verification entry points, and Docker Compose infrastructure.
- Decomposed all 14 product modules into 44 atomic, dependency-aware features with acceptance criteria, ownership, S/M sizing, nature tags, and QA status.
- Added a standard-library backlog checker for exact fields, IDs, dependency existence/cycles, sizing, and QA evidence invariants.
- Created module tracking issues #5–#18 and closed Session 0B planning issue #3.
- Investigated PR #1 Actions: CI passed. Security failed because dependency review requires repository dependency graph and CodeQL was configured for Go/JS before source files existed.
- Added Go API walking-skeleton code: test-only OTP boundary, PostgreSQL-backed idempotent order creation, guarded state transitions, simulated double-entry ledger, append-only constraints, and transactional outbox schema.
- Added Go domain tests and CI Go formatting/race-test steps; scoped CodeQL to Go and Python.

## Verification
- PR #1 CI passed; Security failure causes are confirmed from Actions logs.
- PR #19 (Session 0B) has been opened; its Actions result is not yet available.
- Session 0C code has not been run locally or by CI yet. Do not treat it as verified.
- No product feature is marked independently QA-passing.

## Human/repository gates
- Enable dependency graph, then set `DEPENDENCY_GRAPH_ENABLED=true` to activate dependency review.
- Configure rulesets, Actions permissions, Dependabot, secret scanning/push protection, and verified CODEOWNERS in repository settings.
- Choose a license before the first public release.
- Confirm launch jurisdiction before enabling payment, KYC, tax, and labor workflows.

## Next
1. Obtain passing CI for PR #19 and the Session 0C PR; fix all failures.
2. Complete the outbox publisher/idempotent consumer and real E2E smoke path before closing issue #4.
3. Deliver M1–M14 features in dependency order; only independent QA may mark features passing.
