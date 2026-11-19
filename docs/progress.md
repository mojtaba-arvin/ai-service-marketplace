# Development Progress

> Keep this file concise (maximum 60 lines). Update it at the end of each work session.

## Current phase
Session 0B complete. Session 0C implementation is committed to a draft PR and awaits CI verification.

## Completed
- Established repository foundation, agent guidance, ADRs, module placeholders, verification entry points, and Docker Compose infrastructure.
- Decomposed all 14 product modules into 44 atomic, dependency-aware features with acceptance criteria, ownership, S/M sizing, nature tags, and QA status.
- Added a standard-library backlog checker and 14 module tracking issues (#5–#18); closed planning issue #3.
- Inspected PR #1 Actions: CI passed. Security failed because dependency review requires repository dependency graph and CodeQL was configured for Go/JS before source files existed.
- Added Go API walking skeleton: test-only OTP, PostgreSQL idempotent orders, guarded transitions, simulated balanced append-only double-entry ledger, transactional outbox, and idempotent outbox consumer.
- Added unit/integration coverage and HTTP E2E script for OTP → order → replay → state transition → invalid transition.
- CI now provisions PostgreSQL, runs Go tests with race detection, executes HTTP E2E, and scans Go/Python with CodeQL.

## Verification
- PR #1 CI passed; Security failure causes were confirmed from Actions logs.
- PR #19 (Session 0B) and PR #20 (Session 0C) are open draft PRs. Their latest CI runs have not yet been confirmed.
- Local clone/test execution was blocked because this environment cannot resolve github.com. Do not treat the new API as verified until Actions pass.
- No product feature is marked independently QA-passing.

## Human/repository gates
- Enable dependency graph, then set `DEPENDENCY_GRAPH_ENABLED=true` to activate dependency review.
- Configure rulesets, Actions permissions, Dependabot, secret scanning/push protection, and verified CODEOWNERS in repository settings.
- Choose a license before the first public release.
- Confirm launch jurisdiction before enabling payment, KYC, tax, and labor workflows.

## Next
1. Obtain passing CI for PR #19 and #20; fix all failures before marking either ready.
2. Close issue #4 only after CI proves the publisher/consumer and clean-checkout E2E acceptance criteria.
3. Deliver M1–M14 features in dependency order; only independent QA may mark features passing.
