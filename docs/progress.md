# Development Progress

> Status snapshot: 2026-11-22. Update after each engineering session. Never mark a feature QA-passing without independent evidence.

## Project direction

This is an open-source, reusable home-services marketplace platform. Geographic context (Armenia and the GCC countries) informs localization, configuration, and compatibility scenarios; it is not a go-to-market plan, a launch sequence, or permission to enable regulated production integrations.

The long-term engineering stages and exit gates are documented in [the engineering roadmap](ROADMAP.md). This file records the evidence-backed current state; the roadmap is not a substitute for issue-level acceptance criteria.

## Integrated on main

The current main baseline includes PR #86 (`e3ff27c09104a63c7ab79b243dddbd127b0f4c7f`) and PR #90 (`9db26f97306747bb020a2110ed43060b8622b635`).

- Go modular-monolith API and PostgreSQL persistence.
- Customer/professional/operator API vertical slice and responsive Next.js browser application.
- Browser session hardening using HttpOnly cookies and CSRF/Origin protections.
- One-shot migration runner with PostgreSQL advisory-lock serialization, transactional migrations, replay safety, and concurrency/rollback/retry integration coverage.
- Production-oriented container and Compose deployment foundation with Caddy HTTPS ingress.
- Browser E2E journey covering customer booking, operator verification/dispatch, professional acceptance, order status, cookie/CSRF/refresh/sign-out behavior.
- CI rehearsal for PostgreSQL backup/restore and transactional outbox behavior.
- CI, CodeQL, dependency review, repository ruleset, and agent guard workflows are present.

These are repository-integrated capabilities. They do not establish that the application is running on a public production host or that every feature has passed independent QA.

## Current release-foundation state

- Repository-side deployment assets and migration safety work are integrated. Real-host provisioning, DNS, production secrets, OTP delivery credentials, public smoke tests, and backup/restore against actual production storage remain external deployment tasks tracked in #56.
- Independent end-to-end and negative-path release verification remains tracked in #57.
- The parent MVP vertical-slice issue #53 remains open until its stated deployment and independent verification criteria are met.
- Real payment capture, escrow movement, settlement, and production KYC are not enabled by the current MVP. Simulated ledger behavior and test-only identity paths must never be presented as production integrations.
- An external outbox sink is optional and is not configured in the repository baseline.

## Backlog and QA discipline

- The feature inventory in `feature_list.json` is the structured source of truth for feature acceptance and QA state.
- Implementation merged is not equivalent to independently verified.
- Keep `passes: false` until an independent verification workflow records evidence and the verifier identity.
- Module tracker issues #5–#10 and #18 remain useful coordination records; reconcile them against the current feature inventory before changing their status.
- Do not remove deferred capabilities merely because they are outside the current vertical slice.

## Security and repository operations

- The active `protect main` ruleset was confirmed on 2026-11-22. Required checks and branch protections are documented in issue #2.
- Dependency Review succeeded on PR #84 (Security run #45001472857); its post-merge skip in run #45002061644 is expected because that job is pull-request-only.
- The current security workflow does not require a `DEPENDENCY_GRAPH_ENABLED` Actions variable.
- Dependabot alerts/security updates and repository-level secret scanning/push-protection settings still require owner-side confirmation in GitHub Settings; do not report them as enabled without verification.
- CODEOWNER approval is intentionally not required while the project has a single maintainer. Revisit when an independent maintainer is added.

## Geographic configuration guidance

Use Armenia and Bahrain, Kuwait, Oman, Qatar, Saudi Arabia, and the United Arab Emirates as representative configuration/test contexts when useful. Keep country, currency, locale, language/direction, timezone, and provider configuration explicit and data-driven. These examples do not establish a production launch jurisdiction or imply that payments, KYC, tax, labor, or other regulated workflows are approved.

## Next engineering priorities

1. **Independent release QA (#57):** extend and execute negative-path coverage for authorization, idempotency, expiry, concurrency, migration failures, unavailable dependencies, and data recovery. Record evidence; do not self-certify.
2. **Reproducible deployment (#56):** complete host-agnostic documentation and deployment checks; keep actual host/DNS/credentials as explicit owner-operated steps.
3. **Feature QA:** independently validate implemented features in `feature_list.json`, starting with security- and money-sensitive identity, order, dispatch, ledger, and market-configuration invariants.
4. **Architecture/spec alignment:** ensure module specifications, OpenAPI/event contracts, ADRs, and implementation agree; add missing executable contract tests before expanding scope.
5. **Open-source contributor readiness:** keep setup instructions deterministic, document supported local workflows, clarify contribution/review expectations, and ensure CI can run without live third-party credentials.

## Verification limitations

This snapshot is based on repository files, issue records, and GitHub workflow evidence. No claim is made here that a local checkout was built in this session, that a real host was provisioned, or that independent release QA has passed.

## 2026-11-22 hardening merged and follow-up work

PR [#86](https://github.com/mojtaba-arvin/ai-service-marketplace/pull/86) was squash-merged to `main` as `e3ff27c09104a63c7ab79b243dddbd127b0f4c7f`. CI, Security (including CodeQL and Dependency Review), and Agent Guard passed. It advanced the following workstreams:

- **Release QA/security:** migration `023_order_tenant_scope.sql` binds scheduled orders to a tenant; operator order listing and idempotency replay are tenant-scoped. A same-market cross-tenant regression test is included.
- **Trust/finance:** support case access and writes require active support-agent membership; fake payment provider rejects changed request payloads under a reused idempotency key.
- **Specifications/contracts:** module specs, an initial source-aligned OpenAPI snapshot, a canonical tenant-scoped event schema and example are added. Existing event producers still use legacy payloads; full adoption remains open.
- **Contributor readiness:** `CONTRIBUTING.md` and `SECURITY.md` establish contribution, verification and private vulnerability-reporting guidance.
- **Long-term modules:** #87 tracks analytics/event adoption, #88 tracks B2B organization/property modeling, and #89 tracks adversarial AI evaluation and prompt governance. ADRs 0004 and 0005 record tenant/organization separation and the canonical event envelope.

PR #90 is now merged after CI, Security and Agent Guard passed. It scopes customer history/timeline, professional order reads/transitions and cancellation idempotency to the order tenant, with a same-identity cross-tenant regression. M14 prompt-governance regression work is in PR #91 (CI, Security and Agent Guard passed). The canonical event-writer primitive is in PR #92 (CI verification pending after a validation fix). The B2B schema foundation is in PR #93 (CI/Security pending; Agent Guard passed after removing a prohibited feature-inventory edit). These follow-ups are not considered complete until their current CI and acceptance criteria pass.

No feature inventory item has been marked independently passing. M12 producer/consumer adoption, M13 service/API authorization and M14 adversarial evaluation gates remain incomplete until their focused follow-up work is accepted.
