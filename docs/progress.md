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

PR #90 subsequently closed the identified customer order-history/timeline, professional order-list/transition, and cancellation idempotency tenant-scope gaps. Its same-identity, multi-tenant regression is merged on main. This is code/CI hardening, not completion of the public-deployment and independent release QA still required by #57.

M12 analytics/event adoption (#87) and M14 AI safety evaluation/prompt governance (#89) remain implementation work. M13 (#88) is partially implemented through tenant-scoped organization/property membership, invitation inbox, lifecycle and delegation APIs; order/financial attribution and consolidated statement read models remain gated. Issue status and ADR/spec foundations are not evidence of independent feature acceptance. The feature inventory remains `passes: false` pending independent QA evidence.


## 2026-11-22 PR #98 — tenant-scoped B2B schema foundation

Migration `024_organizations_properties.sql` adds tenant-scoped organizations, organization memberships, properties, property delegations and append-only organization audit events. Composite foreign keys bind creator, inviter, delegate and audit actor identities to memberships in the same tenant and organization. PostgreSQL negative-path coverage rejects cross-tenant organization creators, inviters, property creators, delegation creators, members and audit actors. This is schema groundwork only: service/API authorization, invitation delivery, immediate revocation behavior, property handlers and consolidated statements remain open in #88. CI passing is not independent QA; no feature inventory item is marked independently passing.


## 2026-11-22 M13 organization and membership implementation

The tenant-scoped organization/property API and membership lifecycle are now integrated on `main`:

- PR #100: membership-checked organization reads and role-scoped property visibility.
- PR #101: transactional organization and property creation with initial owner membership, audit and outbox writes.
- PR #103: invitation, acceptance, role changes and revocation; membership changes serialize on the organization row, protect the final active owner, and revoke property delegations in the same transaction.
- PR #102 was closed without merge because its overlapping implementation was superseded by #101 and #103. It must not be merged or treated as a separate API implementation.
- CI, Security (including Dependency Review and CodeQL) and Agent Guard completed successfully for PR #103's exact head. This is automated verification, not independent feature QA.

PR #104 merged an authenticated pending-invitation inbox, PostgreSQL lifecycle coverage, and alignment of the B2B module specification and OpenAPI contract with the implemented routes. The inbox is tenant- and identity-scoped, bounded to 100 newest pending invitations, and does not imply email/SMS delivery.

The feature inventory remains `passes: false` pending independent acceptance. Invitation expiry/re-send/delivery, B2B order attribution, order/ledger aggregation and consolidated billing remain deferred. No production billing or money movement is enabled.



## 2026-11-22 PR #106 — organization/property lifecycle merged

PR [#106](https://github.com/mojtaba-arvin/ai-service-marketplace/pull/106) was squash-merged to `main` as `a45390739ed543a09698a55aa15ad618f9e3183a`.

The merged API adds organization update/archive, property update/archive, and property-delegation list/grant/role-change/revoke operations. Authorization is tenant-scoped and role-checked; mutations serialize on organization/property row locks and commit append-only audit plus canonical outbox events transactionally. Archive is soft and preserves historical organization/property/order/ledger rows.

QA evidence on the exact PR head `fd9b95db49c80e2c58c0fba1ce35f9e6a361c611`:
- CI run [#45020849394](https://github.com/mojtaba-arvin/ai-service-marketplace/actions/runs/45020849394): success, including Go race tests with PostgreSQL 17 and `TEST_DATABASE_URL`, HTTP/browser E2E, Compose checks, and production API/Web image builds.
- Security run [#45020849401](https://github.com/mojtaba-arvin/ai-service-marketplace/actions/runs/45020849401): success, including Dependency Review, Security Detect and CodeQL Go/Python/JavaScript-TypeScript.
- Agent Guard run [#45020849366](https://github.com/mojtaba-arvin/ai-service-marketplace/actions/runs/45020849366): success.
- Agent-led code review checked tenant/organization/property scoping, role restrictions, row-lock serialization, soft archive, audit/outbox transaction boundaries and OpenAPI structure. PostgreSQL lifecycle tests cover update/archive/delegation paths and cross-tenant denials.
- This is agent-led QA evidence, not independent third-party acceptance. No feature inventory flag was changed.

### Remaining M13 work

Issue #88 remains open. The relationship of B2B orders to properties and the finance attribution/read model are now specified in [ADR 0006](adr/0006-b2b-order-finance-attribution.md), but intentionally not implemented. The current order model lacks organization/property attribution; payment operations and ledger journals lack a reliable order/tenant link. Adding these before independent order and finance QA would create unsafe historical attribution. The implementation gates are #57 (order tenant isolation), #10 (finance invariants), and #87 (canonical event adoption). No legacy financial/order backfill, mutable organization balance, production billing or money movement is authorized by this design.


## 2026-11-22 PR #107 — B2B attribution architecture accepted

PR [#107](https://github.com/mojtaba-arvin/ai-service-marketplace/pull/107) was squash-merged as `5f487ad613e45a4ed57d07b5afe5448318905731`.

ADR 0006 now defines the tenant-preserving relationship between organizations, properties, orders, payment operations and immutable ledger postings, including the staged additive migration and rollback approach. It is a design decision only: order and finance attribution, event producers/projections, and consolidated statements remain gated on #57, #10 and #87. No historical attribution is inferred, and no billing or money movement is enabled.

## 2026-11-22 PR #108 — stale property delegation QA fix merged

PR [#108](https://github.com/mojtaba-arvin/ai-service-marketplace/pull/108) was squash-merged as `9ea0e1c9938c6b2b262a33bf8c3f2f3481b87d03`.

Agent-led QA found that changing an organization member/viewer to an ineligible role could leave property delegations active; demoting the identity later could restore visibility without an explicit grant. The fix now revokes active delegations transactionally whenever a role transition crosses into or out of the member/viewer eligibility set. Each revoked property grant receives an append-only audit entry and canonical outbox event. Member<->viewer changes preserve explicit grants; demotion does not reactivate a revoked grant. The delegation list filters ineligible targets.

Regression coverage verifies member/viewer preservation, promotion revocation, no resurrection after demotion, loss of property visibility, and explicit regrant restoring access. The same full CI run also verifies the existing scheduled-order market-negative test now selects a market code confirmed absent from shared fixtures rather than assuming US is unconfigured.

QA evidence on exact PR #108 head `249d0eb68f991c34e5d138e8f7c0de97c5dcf266`:
- CI [#45022503345](https://github.com/mojtaba-arvin/ai-service-marketplace/actions/runs/45022503345): success — PostgreSQL-backed Go race tests, HTTP/browser E2E, Compose validation and production API/Web image builds.
- Security [#45022503484](https://github.com/mojtaba-arvin/ai-service-marketplace/actions/runs/45022503484): success — Dependency Review, Security Detect and CodeQL Go/Python/JavaScript-TypeScript.
- Agent Guard [#45022503404](https://github.com/mojtaba-arvin/ai-service-marketplace/actions/runs/45022503404): success.

This is agent-led QA, not independent third-party sign-off. Feature inventory remains `passes: false`; issue #88 remains open until the gated order/finance relationship and consolidated statement work is implemented and verified.


## 2026-11-23 — Dynamic service questionnaire foundation

- PR #166 introduces a bounded, typed service-form schema/answer validator for admin-defined questionnaires, with strict schema decoding, closed objects, resource limits and negative-path tests.
- This is validation logic only. Version persistence/publication, admin authorization, API/rendering, immutable order snapshots and independent acceptance remain open; CAT-004 is not passing.


## 2026-11-23 — Dispute Triage advisory boundary

A new backend package provides evidence-grounded case summaries, established facts, missing-information checklists and next-step suggestions. It redacts user/evidence text, limits citations to retrieved sources, rejects verdict/refund/settlement fields, always requires human adjudication and includes a deterministic golden evaluator. Persistence, private evidence storage, API/UI, appeals and independent adjudicator integration remain open; TRUST-003 is not passing.
