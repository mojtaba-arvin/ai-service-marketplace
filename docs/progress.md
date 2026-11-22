# Development Progress

> Status snapshot: 2026-11-22. Update after each engineering session. Never mark a feature QA-passing without independent evidence.

## Project direction

This is an open-source, reusable home-services marketplace platform. Geographic context (Armenia and the GCC countries) informs localization, configuration, and compatibility scenarios; it is not a go-to-market plan, a launch sequence, or permission to enable regulated production integrations.

The long-term engineering stages and exit gates are documented in [the engineering roadmap](ROADMAP.md). This file records the evidence-backed current state; the roadmap is not a substitute for issue-level acceptance criteria.

## Integrated on main

The current main baseline includes the merged tenant-isolation and open-source-readiness changes from PR #86 (merge commit `e3ff27c09104a63c7ab79b243dddbd127b0f4c7f`).

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

## 2026-11-22 hardening merged and follow-up audit

PR [#86](https://github.com/mojtaba-arvin/ai-service-marketplace/pull/86) was squash-merged to `main` as `e3ff27c09104a63c7ab79b243dddbd127b0f4c7f`. Its CI, Security (including CodeQL and Dependency Review), and Agent Guard workflows completed successfully. No independent review submissions or inline review threads were present; the merge did not change feature-level QA status.

Merged work includes migration `023_order_tenant_scope.sql`, tenant-scoped operator order listing and scheduled-order idempotency replay, active support-agent membership enforcement, fake-payment idempotency conflict detection, module specs, an initial OpenAPI snapshot, the canonical event-envelope schema, ADRs 0004/0005, and contributor/security guidance.

A post-merge code audit identified additional tenant-boundary gaps outside the original operator queue: customer order history/timeline and professional order/transition paths need to bind the order's tenant as well as the identity/provider membership. Follow-up hardening is being prepared in PR #90. Until that audit passes, tenant isolation must not be described as comprehensively complete.

M12 analytics/event adoption (#87), M13 organization/property registry (#88), and M14 AI safety evaluation/prompt governance (#89) remain implementation work. Their issues and ADR/spec foundations are not evidence that those capabilities are implemented. The feature inventory remains `passes: false` pending independent QA evidence.


## 2026-11-22 PR #98 — tenant-scoped B2B schema foundation

Migration `024_organizations_properties.sql` adds tenant-scoped organizations, organization memberships, properties, property delegations and append-only organization audit events. Composite foreign keys bind creator, inviter, delegate and audit actor identities to memberships in the same tenant and organization. PostgreSQL negative-path coverage rejects cross-tenant organization creators, inviters, property creators, delegation creators, members and audit actors. This is schema groundwork only: service/API authorization, invitation delivery, immediate revocation behavior, property handlers and consolidated statements remain open in #88. CI passing is not independent QA; no feature inventory item is marked independently passing.


## 2026-11-22 M13 organization and membership implementation

The tenant-scoped organization/property API and membership lifecycle are now integrated on `main`:

- PR #100: membership-checked organization reads and role-scoped property visibility.
- PR #101: transactional organization and property creation with initial owner membership, audit and outbox writes.
- PR #103: invitation, acceptance, role changes and revocation; membership changes serialize on the organization row, protect the final active owner, and revoke property delegations in the same transaction.
- PR #102 was closed without merge because its overlapping implementation was superseded by #101 and #103. It must not be merged or treated as a separate API implementation.
- CI, Security (including Dependency Review and CodeQL) and Agent Guard completed successfully for PR #103's exact head. This is automated verification, not independent feature QA.

PR #104 adds an authenticated pending-invitation inbox, PostgreSQL lifecycle coverage, and alignment of the B2B module specification and OpenAPI contract with the implemented routes. The inbox is tenant- and identity-scoped, bounded to 100 newest pending invitations, and does not imply email/SMS delivery.

The feature inventory remains `passes: false` pending independent acceptance. Property delegation management, property update/archive, invitation expiry/re-send/delivery, order/ledger aggregation and consolidated billing remain deferred. No production billing or money movement is enabled.


## 2026-11-22 M13 organization/property lifecycle follow-up

Implementation is proposed in [PR #106](https://github.com/mojtaba-arvin/ai-service-marketplace/pull/106) on `agent/88-organization-property-lifecycle`, based on the post-PR-104 `main` baseline. The branch adds organization update/archive, property update/archive, and property-delegation list/grant/role-change/revoke operations. Archive operations are soft and retain historical rows; organization archive archives child properties and revokes all memberships/delegations, while property archive revokes only its own delegations. All mutations are tenant-scoped, lock the organization/property rows, and write append-only audit plus canonical outbox events transactionally.

Authorization policy: organization updates require owner/admin; organization archive is owner-only; property update/archive require owner/admin/manager; owner/admin can manage manager/viewer property delegations; organization managers may grant/revoke viewer only. Delegation roles currently provide visibility only. PostgreSQL lifecycle integration tests, the complete CI workflow, Security (Dependency Review, Security Detect and all three CodeQL languages), and Agent Guard passed on PR #106 head `2c6935e`. Independent feature QA remains pending; no feature inventory status is changed and no independent QA claim is made.
