# Development Progress

> Update at the end of each engineering session. Do not mark features passing without independent QA evidence.

## Current phase

The API, responsive browser MVP, browser-level E2E journey, and disposable PostgreSQL backup/restore rehearsal are integrated into `main` for one configured market. CI and CodeQL passed on the post-merge commit. The project is not deployed to a live host; production smoke/restore and independent release QA remain open.

## Completed foundation

- Repository harness, ADRs, agent instructions, playbooks, issue/PR templates, module scaffolds, Docker Compose, CI, and security workflows.
- 44 atomic backlog features across all 14 modules, with acceptance criteria, dependencies, S/M sizing, nature tags, and QA status. Issues #11–#17 are closed as not planned for the current MVP; their feature records remain intact.
- Backlog checker validates feature shape, unique IDs, dependency references/cycles, sizing, and QA evidence invariants.
- PostgreSQL walking skeleton: test-only OTP/auth, idempotent order creation, guarded state transitions, transactional outbox, idempotent outbox consumer, simulated balanced append-only ledger, and HTTP E2E.
- Backend integration baseline merged through PR #58.
- Single-host deployment foundation merged through PR #60.
- Production-safe customer/professional/operator API vertical slice merged through PR #61 (merge commit `1f022a236c8cae4bf0bcb39d061f3b5d412d0c09`).
- 29 of 44 feature records have implementation merged. MSG-003 has partial transport groundwork; remaining 14 features are not implemented. No feature is marked QA-passing.

## Current MVP delivery

- API: merged to `main` via PR #61. CI and CodeQL passed; dependency-review was skipped because the repository dependency graph is not enabled.
- Web: merged via PR #62 (merge commit `58678ae2b3c03b066e574e40afc1f2c79afaa80c`). Production web image build passed with Next.js 15.5.29 and React/React DOM 19.1.10. PR #78 adds the npm v3 lockfile and changes the dependency stage to copy both manifest files and run `npm ci`.
- Web security boundary: sessionStorage token storage is temporary. Move to Secure, HttpOnly, SameSite cookies and add reviewed CSRF protection before broad public launch.
- Deployment: production Compose, non-root API/worker image, Caddy HTTPS ingress, and deployment/backup/restore instructions are merged. Actual host, DNS, OTP provider credentials, peppers, smoke test, and backup/restore validation remain external deployment work in #56.
- Release QA: independent end-to-end and negative-path validation remains open in #57. No public release or independent QA sign-off is claimed.

## Verification evidence

- PR #61: Go formatting/tests, repository verification, PostgreSQL integration, HTTP E2E, Compose validation, production API image build, CodeQL Go/Python passed. Dependency-review skipped pending dependency-graph enablement.
- PR #62: corrected customer JSX grouping and strict TypeScript nullability errors (currency fraction metadata and market timezone); production web image build passed on Next.js 15.5.29. CI, CodeQL Go, Python, and JavaScript/TypeScript passed. Dependency-review skipped pending dependency-graph enablement.
- PR #72: aligned Go CI/API image toolchain with Go 1.25, upgraded `golang.org/x/crypto` to `0.52.0`, and integrated browser journey plus disposable PostgreSQL backup/restore/outbox rehearsal. PR CI and CodeQL passed before merge.
- PR #78: generated `apps/web/package-lock.json` on a GitHub-hosted runner (npm lockfileVersion 3); `npm ci --ignore-scripts --no-audit --no-fund`, `npm run build`, and the production Docker image build using `npm ci` all passed in bootstrap run `44996862114`. The temporary bootstrap workflow removed itself after committing the lockfile and Dockerfile change.
- Post-merge commit `406c2f968c5c0e24d2ccb9fe75d12546d08385a5`: CI run `44994610354` passed; Security run `44994610357` passed for `detect` and all three CodeQL jobs, while `dependency-review` was skipped; Dependabot Go module graph update run `44994614747` succeeded.
- Security language detection now scans Go, Python, and JavaScript/TypeScript.
- No local checkout/test execution or real-host deployment was available in this session.

## Owner decisions and repository settings

- Enable the repository dependency graph and set `DEPENDENCY_GRAPH_ENABLED=true`; the post-merge Dependabot Go module graph update succeeded, but this does not prove the repository-wide dependency graph setting or the Actions variable is enabled.
- An active `protect main` ruleset blocks deletion and non-fast-forward updates; required PR/CI/Security checks and Actions permissions still need configuration.
- Enable Dependabot alerts/security updates and secret scanning/push protection where available.
- `.github/CODEOWNERS` now assigns the verified repository owner `@mojtaba-arvin`; mandatory code-owner approval is not enabled.
- MIT license is present in the repository.
- Confirm launch jurisdiction before enabling regulated payment, KYC, tax, or labor-related workflows.

## Remaining release work

1. Extend and independently review the integrated browser-level E2E journey; automated CI rehearsal is present, but independent release QA remains open in #57.
2. Replace temporary sessionStorage tokens with Secure, HttpOnly, SameSite cookies and add reviewed CSRF protection before broad public exposure.
3. Provision the actual host, DNS, OTP delivery provider, and production secrets through #56.
4. Run independent authorization, idempotency, expiry, concurrency, failure, backup/restore, and deployed smoke tests through #57.
5. Continue independent QA for module trackers #5–#10 and #18; keep `passes: false` until evidence is recorded.

## Deferred product backlog

- Implemented but not independently QA-passed: identity, catalog, pricing, orders, dispatch, finance primitives/contracts, TRUST-001, MSG-001/002, SUP-001, and PLAT-001–004.
- Partial: MSG-003 has an HTTPS outbox publisher groundwork; notification preferences and complete adapters remain.
- Not implemented: TRUST-002–004, SUP-002, ADMIN-001–003, GROW-001–002, AN-001–002, and B2B-001–003.
- Financial invariants remain mandatory: integer minor units, balanced immutable double-entry journals, idempotency, and compensating reversals. No live payment capture, escrow, refund, or settlement is enabled in the MVP.
- AI suggestions must never authorize identity/safety decisions or set payable prices.


## 2026-11-22 hardening work in review

PR [#86](https://github.com/mojtaba-arvin/ai-service-marketplace/pull/86) is open and not merged. It advances five engineering workstreams:

- **Release QA/security:** migration `023_order_tenant_scope.sql` binds scheduled orders to a tenant; operator order listing and idempotency replay are tenant-scoped. A same-market cross-tenant regression test is included.
- **Trust/finance:** support case access and writes require active support-agent membership; fake payment provider rejects changed request payloads under a reused idempotency key.
- **Specifications/contracts:** module specs, an initial source-aligned OpenAPI snapshot, a canonical tenant-scoped event schema and example are added. Existing event producers still use legacy payloads; full adoption remains open.
- **Contributor readiness:** `CONTRIBUTING.md` and `SECURITY.md` establish contribution, verification and private vulnerability-reporting guidance.
- **Long-term modules:** #87 tracks analytics/event adoption, #88 tracks B2B organization/property modeling, and #89 tracks adversarial AI evaluation and prompt governance. ADRs 0004 and 0005 record tenant/organization separation and the canonical event envelope.

CI initially caught a migration-registry drift after migration 023 was introduced. The migration runner's ordered registry has now been updated; the CI rerun is pending. A local checkout was unavailable, so no local test pass is claimed.

No feature inventory item has been marked independently passing. PR #86 must pass CI and receive independent acceptance before these changes are treated as integrated.
