# Development Progress

> Rolling evidence snapshot: 2026-11-25 (updated after PR #210 and TRUST-002 intake implementation). Git history and PRs retain detailed history. Never mark a feature QA-passing without independent evidence.

## Product direction

AI Service Marketplace is a complete open-source, self-hostable, multi-market home-services platform spanning M1–M14. It is not an MVP, demo-only product or single vertical slice. See [README](../README.md), [Product Scope](product-scope.md), [Business Vision](vision/business-plan.md), [Technical Plan](vision/tech-plan.md) and [Roadmap](ROADMAP.md).

## Integrated on main

- Main at snapshot: `88ec53e396fb411dfe7930b218b76cf478c1ec60` (after PR #213).
- PRs #146, #150, #149, #152, #148, #193, #194, #195, #196, #197, #198, #199, #200, #201, #202, #203, #204, #206 and #208 are merged. #205 and #207 were closed as superseded by refreshed PRs #206 and #208. #148 adds deterministic AI timeout, caller-cancellation and output-bound tests; it is not independent AI safety acceptance. #195/#197 record analytics runtime and release-gate decisions; #198 delivers the historical rebuild/rollback command; #199 records that delivery and its remaining gates; #200 refreshes deployment and analytics operations guidance.
- PR #194: migration 030, tenant/generation-scoped immutable analytics facts and quarantine, per-tenant active-generation pointer, bounded live analytics worker, active-generation-only summaries with pending-event readiness, production image and optional Compose profile. PostgreSQL integration tests cover restart, generation mismatch, idempotency, shadow isolation and pointer cutover; CI/E2E/Security/CodeQL/Agent Guard passed on exact head.
- PR #196: authenticated `GET /v1/operator/analytics/summary`, operator-only `analytics:read`, tenant taken exclusively from the authenticated principal, RFC3339 bounded-window validation, `Cache-Control: private, no-store`, OpenAPI/metric contracts, and PostgreSQL privacy/quarantine/immutability/idempotency plus route-authorization tests. Exact-head Go tests, E2E, Compose validation, API/web image builds, Security/CodeQL and Agent Guard passed.
- PR #200: corrects deployment evidence and adds a guarded analytics rebuild/cutover/worker-handoff/rollback runbook. It is documentation only; no production deployment or analytics operation is claimed.
- PR #201: reconciles Golden Dataset v2 and Prompt Governance documentation, separating offline evaluator results from live-model acceptance.
- PR #202: records the existing tenant-scoped append-only support action audit implementation (SUP-002) with code, migration and test evidence; independent QA remains pending.
- Go/PostgreSQL modular-monolith API; Next.js customer web; React/Vite operator UI; Flutter mobile direction; isolated Python AI workloads.
- Identity/tenant memberships, customer order workflows, professional eligibility, scheduling, availability, dispatch, cancellation, history and transactional outbox.
- Immutable ledger primitives, payment adapter boundaries, market configuration/localization, B2B foundations, migrations, Compose deployment, CI, E2E, backup/restore, CodeQL, Dependency Review and Agent Guard.
- PR #156: complete fourteen-domain product definition, adopter-facing README, business/technical vision and dependency-aware roadmap.
- PRs #159/#162: grounded Professional Copilot and append-only market-configuration audit with before/after snapshots.
- PRs #168/#180: dynamic service-form validation and market/locale version publication lifecycle.
- PR #171: initial 77-feature inventory; all entries remained passes=false.
- PRs #175/#177: session-bound device inventory/revocation and evidence-grounded Dispute Triage advisory boundary.
- PR #181 (`6167b8fcbf79cbe24e26296237373453ab701910`): published-form answer validation, immutable order snapshots and semantic idempotent replay.
- PR #182 (`57ad0061c4f621a141b2169ccce5534cf8f4321e`): corrected full-product language, web security docs and deployment/contract boundaries.
- PR #183 (`2eada2b6242276c0719e778ab5d77aa743418bb0`): recursive customer questionnaire renderer and structured answer submission.
- PR #184 (`f7431d3570030f6ac0d4d70bc4c9fab76e0df7f0`): expanded the inventory from 79 to 93 features with 14 additional M1–M14 capabilities; updated module specs and issue epics.
- SUP-002 is already implemented with SUP-001 in `support_cases.go` and migration `021_support_cases.sql`: case creation, assignment and status transitions append actor/action/reason/before-after audit events transactionally; PostgreSQL rejects UPDATE/DELETE. `support_cases_test.go` exercises lifecycle, inactive-agent and ownership denials, invalid transitions and event counts. Independent QA remains pending; do not mark passes=true.

These are repository capabilities, not evidence that every domain is complete or that a production marketplace is publicly deployed.

## Product workstreams and gates

1. **M1 Identity/privacy:** trusted-device attestation/step-up policy; privacy export/deletion only under approved policy.
2. **M2 Catalog/pricing:** form lifecycle and order snapshots, immutable price rules, packages/subscriptions and localized SEO.
3. **M3/M4 Customer/professional:** intake, scheduling, estimates, tracking, masked communications, completion/variations/reviews/reorder; onboarding/KYC/academy, quotes, earnings, CRM and offline-safe mobile.
4. **M5 Matching:** broadcast/sequential/customer-choice strategies, reassignment, capacity and advisory demand/supply forecasts.
5. **M6 Finance:** escrow/holds, payouts/refunds/chargebacks, tax/invoices/reconciliation; gated on independent Order/Ledger QA.
6. **M7/M8 Trust and communications:** SOS/claims/disputes/moderation/audit, consent-separated channels, protected chat and support copilot.
7. **M9/M10 Admin and growth:** guarded state machine, step-up approval, queues/dashboards, SEO/referrals/campaigns/experiments/lifecycle.
8. **M11/M12 B2B and data:** property-attributed orders/ledger, contracts/webhooks, canonical events, replayable projections and warehouse/dashboards.
9. **M13/M14 Platform and AI:** market/adapters, privacy/DR/observability, Intake/Pro/Dispute/Support/Fraud/Quality/Forecast AI.

## Hard blockers and safety boundaries

- #110: external consumer compatibility for canonical scheduled-order events; include optional `service_form_version_id` from #181 in the consumer compatibility review. Do not infer external compatibility from internal publisher/timeline tests.
- #133 is closed as superseded by #194 and #196. Do not reopen or reintroduce its migration 025 or duplicate projector/summary package. Its former API route/contracts work was reconstructed against current main and merged in #196.
- PR #194 provides live polling without a monotonically advancing ID watermark: query bounded outbox rows absent from the generation-specific `processed_events`, then call transactional `Consumer.Process`. Keep this pattern; PostgreSQL sequence IDs can commit out of order.
- PR #196 exposes the summary API, but `data_ready` is an ingestion-lag indicator only: it is not event-time finality, a late-arrival watermark, or a finance/contractual close signal.
- PR #198 (`c322bad528d84753c64bc7ecfad12b76d84540ee`) adds the one-shot shadow-generation rebuild/rollback command. It uses generation-scoped `processed_events` polling without an ID watermark, tenant-scoped advisory locking, source/processed/fact/quarantine one-to-one validation, source-payload SHA-256 verification, a guarded pending-event check, and an atomic generation-state/active-pointer cutover. Rollback catches a retained retired generation up, validates it, then atomically restores its pointer. The production image packages the command and Compose/runbook describe the one-shot workflow. Exact-head CI, PostgreSQL integration, Security, all CodeQL languages, and Agent Guard passed before merge. This closes the repository implementation gap for historical rebuild, but not operational proof: no real production rebuild/cutover, live-worker handoff observation, load rehearsal, independent analytics acceptance, or metric late-arrival/finality policy is claimed. Keep AN-001/AN-002 `passes:false` until independent acceptance and metric semantics are signed off.
- #137: canonical cancellation-event compatibility. Preserve legacy payloads for existing event types; add canonical data additively or under a versioned event contract, with old/new contract tests and a recorded consumer migration plan. Repository docs identify only a generic optional HTTPS sink; no actual sink/owner/contract is recorded.
- B2B billing waits for independent Order/Ledger gates (#57/#10) and canonical event readiness (#87).
- No retention duration, privacy policy, fairness threshold, tax/legal rule or regulated-provider approval is invented.
- GitHub security settings require owner-side confirmation; AI affecting money, safety, identity, access or reputation remains advisory until deterministic policy and/or authorized human approval.
- Fake adapters are for local/CI only; no real payment capture, KYC or production messaging is implied.

## Acceptance and execution rules

- `feature_list.json` is the structured 93-feature inventory; as of 2026-11-25, 0/93 are independently QA-passing. This is an acceptance-evidence count, not an implementation count. Do not set passes=true from CI alone.
- Keep module ownership and public API/event boundaries; use small independent PRs and work on other tasks while CI runs.
- Required checks must pass on current PR head and current main base. Do not bypass branch protection, security gates or independent QA.
- `progress.md` records evidence; roadmap records intended outcomes. Neither substitutes for issue-level acceptance.
- Local GitHub CLI access from this runtime was unavailable (DNS); GitHub-hosted CI is the execution evidence. No local test success is claimed.

## Latest delivery — 2026-11-25

- PR #198 merged as `c322bad528d84753c64bc7ecfad12b76d84540ee` after exact-head CI, PostgreSQL integration, Security/CodeQL and Agent Guard all passed.
- PR #200 merged as `61d3528ad2ca451aad841e831ebd46a5d9d474c7`; deployment and analytics runbooks now reflect the integrated repository evidence and retain explicit external-operation gates.
- PR #201 merged as `9bc0ae14338b340868517766e3f9a8325a1e9513`; Golden Dataset v2 and Prompt Governance evidence is clarified.
- PR #202 merged as `8022c689c5183ae85b12da22406f0ce283d74315`; SUP-002 implementation evidence is reconciled without changing independent acceptance status.
- The rebuild tool is a guarded one-shot operation, not an automatic deployment hook. Never execute it against production without a tenant/generation-specific change plan, a fresh generation ID, a database backup/restore checkpoint and a monitored worker handoff.
- The old generation is retained for rollback. Do not add automated generation deletion until retention, privacy and recovery policies are explicitly defined.
- The rebuild command catches events committed before the cutover snapshot; events committed after cutover are expected to be consumed by the new live worker after it is restarted with the new generation. Production cutover still needs an observed end-to-end run.
- `data_ready` continues to mean ingestion backlog drained only; it is not event-time finality, financial close, or contractual completion.


## Latest delivery — 2026-11-25 (ADMIN-001 / TRUST-002)

- PR #203 refreshed this progress snapshot through PR #202; PRs #204 and #206 then merged the ADMIN-001 and TRUST-002 architecture decisions (ADRs 0007 and 0008).
- PR #208 merged as `b2e961d91c0362b98c3f20b8dbbd450d9338c30a` after exact-head CI, Security (including all CodeQL jobs) and Agent Guard succeeded. It adds migration 031 for tenant-scoped operator action grants, a one-time per-tenant bootstrap marker, append-only permission audit events, and a deployment-only bootstrap command packaged in the API image.
- PR #208 is provisioning infrastructure only. ADMIN-001 is **not** complete: persisted grants are not yet enforced at every sensitive Store boundary; runtime grant/revoke operations, no-self-grant authority, transaction-aware authorization, and concurrency/negative tests remain. Existing operator memberships are deliberately not grandfathered.
- TRUST-002 intake is implemented in PR #211: order-bound reports, reporter-only reads, append-only audit, bounded fields and opaque evidence references. The reviewer workflow follows ADR 0008 and PR #214's explicit same-tenant `incident:review` grant; private evidence storage/access and adopter-configured retention remain separate boundaries. Do not imply emergency monitoring or guaranteed response.
- PRs #205 and #207 are superseded and closed. Their old branches may be deleted; refreshed PR branches #206 and #208 contain the retained work.
- No acceptance flags changed: `feature_list.json` remains 0/93 independently QA-passing. CI proves repository checks, not independent product acceptance or production operation.

## Current execution priorities

1. **P0 — Deployment Foundation / Core QA:** repository assets and CI rehearsals are integrated, but a real host, DNS, OTP delivery, production secrets, deployment, public smoke test, backup/restore and independent end-to-end QA are still external gates.
2. **P1 — ADMIN-001 operational acceptance:** runtime grant enforcement and tenant-scoped grant/revoke APIs are merged in PR #213; validate real tenant bootstrap, permission-manager recovery and independent acceptance.
3. **P1 — TRUST-002 reviewer workflow:** implement the `incident:review` grant, same-tenant reviewer assignment, forward-only status transitions and reasoned audit before opening report mutation APIs.
4. **P1 — Event Compatibility:** #110 and #137 remain blocked on external consumer compatibility evidence and a migration window. Do not change or merge payload contracts without it.
5. **P1 — Analytics / Support Audit:** rebuild/rollback and support audit code are integrated; real operational execution and independent acceptance remain.
6. **P2 — Remaining M1–M14 scope:** continue the complete product roadmap; do not collapse it into the initial workflow baseline.

## Persistent engineering decisions

- ADMIN-001: RoleOperator membership is eligibility, not authorization. Sensitive actions require a persisted `(identity_id, tenant_id, action)` grant. Keep `Actor` transport-neutral; enforce in the Store/application transaction that performs the protected mutation. Unknown actions and absent/revoked grants deny. No implicit grandfathering and no self-grant.
- Bootstrap: one-time, explicit deployment-controlled action set and reason, existing operator membership required, serializable transaction and tenant lock, immutable audit. Never bootstrap through OTP, login, HTTP, CI or normal application startup.
- Revocation consistency: authorization and protected mutation must share one transaction/locking boundary so a concurrent revoke cannot race past a sensitive operation.
- TRUST-002: incident intake is not an emergency response channel. Derive tenant from the persisted order, authorize reporters against order participation, and never accept a tenant ID from request input. AI may advise but must not decide severity, liability, refund or closure.
- Event contracts: retain #110/#137 branches while external compatibility is unresolved. Internal tests are not evidence of consumer migration approval.
- Do not mark any feature `passes=true` from CI, merged code or architecture documentation alone.


## Latest delivery — 2026-11-25 (ADMIN-001 audit integrity / TRUST-002 intake)

- PR #210 merged with merge commit `73a0a32c02afdcdff78fc3de5e89b626dc401a04`. It adds migration 032, a composite unique key and FK binding each permission audit event to the exact grant tuple `(grant_id, tenant_id, identity_id, action)`. This closes the audit-tuple integrity prerequisite only.
- PR #211 merged as `f3197510b94ab7b29d348cd97d0a58b3cd4cb95b` after CI, Security/CodeQL and Agent Guard passed on exact head `a43b76295e916dbd9902c48c5cbfa9b0fca68e78`. It contains migration 033 and the first TRUST-002 order-bound incident intake slice. Reviewer workflow, private evidence storage/access, retention and independent acceptance remain out of scope.
- PR #212 merged the progress snapshot documentation.
- PR #213 merged as `88ec53e396fb411dfe7930b218b76cf478c1ec60` after CI, PostgreSQL tests, browser E2E, Compose, production image builds, all CodeQL languages and Agent Guard passed on exact head `3f358b0d616cc6d71316e18490082a55db0b46fa`. It adds explicit transaction-bound operator grants, one-way revocation, audit, tenant-scoped grant-management APIs and least-privilege E2E bootstrap.
- ADMIN-001 runtime enforcement is implemented in PR #213: transaction-bound checks cover operator order/provider/analytics reads; market and catalog configuration; service-form publishing; provider verification, availability and booking; dispatch candidate/offer operations; cancellation-policy writes; and professional-verification review/evidence reads. Pure `Authorize` denies operator actions without persisted grant context. The tenant-scoped grant history/create/revoke API requires `operator:permissions:manage`; self-grant and self-revocation are denied. Independent acceptance remains pending.
- PR #110 and #137 remain open and blocked on external consumer compatibility evidence. Keep their branches.
- No feature acceptance flags changed: `feature_list.json` remains 0/93 independently QA-passing. CI and merged code are not independent acceptance evidence.


- PR #214 is the reviewer-workflow ADR update. It specifies a dedicated `incident:review` grant, same-tenant active reviewers, forward-only state transitions, reasoned assignment/severity changes and same-transaction audit. Reviewer endpoints and mutable report transitions are not implemented yet.


## Current operational priorities after PR #213 — 2026-11-25

1. TRUST-002 reviewer workflow: implement the dedicated `incident:review` action, active same-tenant reviewer assignment, forward-only state transitions and DB-enforced same-transaction audit.
2. TRUST-002 evidence boundary: design private object storage, short-lived access, content validation, access audit and adopter-configured retention/legal hold separately.
3. Deployment/Core QA: execute against real adopter infrastructure and independent QA; GitHub CI cannot provide host, DNS, OTP provider, production secrets or backup-restore acceptance.
4. Analytics/Support: execute the merged runbooks and retain operational evidence.
5. Event compatibility #110/#137: await external consumer compatibility approval; do not change event contracts without it.

Independent acceptance remains `0/93`. No feature pass flag is changed by these merges.
