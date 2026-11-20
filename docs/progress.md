# Development Progress

> Update at the end of each engineering session. Do not mark features passing without independent QA evidence.

## Current phase

MVP release candidate assembly for one configured market. The integrated Go/PostgreSQL backend is on `main`; the production-safe API vertical slice has merged. The browser MVP is under final CI repair and review. Deployment assets exist, but no live host deployment or independent release QA is claimed.

## Completed foundation

- Repository harness, ADRs, agent instructions, playbooks, issue/PR templates, module scaffolds, Docker Compose, CI, and security workflows.
- 44 atomic backlog features across all 14 modules, with acceptance criteria, dependencies, S/M sizing, nature tags, and QA status; module tracking issues #5–#18.
- Backlog checker validates feature shape, unique IDs, dependency references/cycles, sizing, and QA evidence invariants.
- PostgreSQL walking skeleton: test-only OTP/auth, idempotent order creation, guarded state transitions, transactional outbox, idempotent outbox consumer, simulated balanced append-only ledger, and HTTP E2E.
- Backend integration baseline merged through PR #58.
- Single-host deployment foundation merged through PR #60.
- Production-safe customer/professional/operator API vertical slice merged through PR #61 (merge commit `1f022a236c8cae4bf0bcb39d061f3b5d412d0c09`).
- 29 of 44 feature records have implementation merged. MSG-003 has partial transport groundwork; remaining 14 features are not implemented. No feature is marked QA-passing.

## Current MVP delivery

- API: merged to `main` via PR #61. CI and CodeQL passed on the PR head; dependency-review was skipped because the repository dependency graph is not enabled.
- Web: PR #62 remains open. Customer-workspace JSX build failure was fixed; Next.js upgraded to 15.5.29 (November 2026 security release) and React/React DOM to 19.1.10. New CI is running against the updated head.
- Web security boundary: sessionStorage token storage is temporary. Move to Secure, HttpOnly, SameSite cookies and add reviewed CSRF protection before broad public launch.
- Deployment: production Compose, non-root API/worker image, Caddy HTTPS ingress, and deployment/backup/restore instructions are merged. Actual host, DNS, OTP provider credentials, peppers, smoke test, and backup/restore validation remain external deployment work in #56.
- Release QA: independent end-to-end and negative-path validation remains open in #57. No public release or independent QA sign-off is claimed.

## Verification evidence

- PR #61: Go formatting/tests, repository verification, PostgreSQL integration, HTTP E2E, Compose validation, production API image build, CodeQL Go/Python passed. Dependency-review skipped pending dependency-graph enablement.
- PR #62 previous head failed only at the production web image build due to malformed JSX in `apps/web/src/app/page.tsx`. The JSX was corrected and dependencies were upgraded; the new workflow run must pass before merge.
- PR #62 dependency-review remains skipped pending dependency-graph enablement.
- No local checkout/test execution or real-host deployment was available in this session.

## Owner decisions and repository settings

- Enable the repository dependency graph and set `DEPENDENCY_GRAPH_ENABLED=true`.
- An active `protect main` ruleset blocks deletion and non-fast-forward updates; required PR/CI/Security checks and Actions permissions still need configuration.
- Enable Dependabot alerts/security updates and secret scanning/push protection where available.
- `.github/CODEOWNERS` now assigns the verified repository owner `@mojtaba-arvin`; mandatory code-owner approval is not enabled.
- MIT license is present in the repository.
- Confirm launch jurisdiction before enabling regulated payment, KYC, tax, or labor-related workflows.

## Remaining release work

1. Get PR #62's updated CI and CodeQL checks green; review and merge the browser MVP.
2. Retarget/maintain the web PR on `main` (API PR #61 has been merged; its temporary base branch was auto-deleted).
3. Complete cookie-based session handling and CSRF review before broad public exposure.
4. Provision the actual host, DNS, OTP delivery provider, and production secrets through #56.
5. Execute the full customer booking → eligible professional offer → single acceptance → order timeline workflow.
6. Run independent authorization, idempotency, expiry, concurrency, failure, backup/restore, and deployed smoke tests through #57.
7. Update module issues #5–#18 with implementation status; keep them open where independent QA or remaining features are outstanding.

## Deferred product backlog

- Implemented but not independently QA-passed: identity, catalog, pricing, orders, dispatch, finance primitives/contracts, TRUST-001, MSG-001/002, SUP-001, and PLAT-001–004.
- Partial: MSG-003 has an HTTPS outbox publisher groundwork; notification preferences and complete adapters remain.
- Not implemented: TRUST-002–004, SUP-002, ADMIN-001–003, GROW-001–002, AN-001–002, and B2B-001–003.
- Financial invariants remain mandatory: integer minor units, balanced immutable double-entry journals, idempotency, and compensating reversals. No live payment capture, escrow, refund, or settlement is enabled in the MVP.
- AI suggestions must never authorize identity/safety decisions or set payable prices.
