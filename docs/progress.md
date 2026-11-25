# Development Progress

> Rolling evidence snapshot: 2026-11-25. Git history and PRs retain detailed history. Never mark a feature QA-passing without independent evidence.

## Product direction

AI Service Marketplace is a complete open-source, self-hostable, multi-market home-services platform spanning M1–M14. It is not an MVP, demo-only product or single vertical slice. See [README](../README.md), [Product Scope](product-scope.md), [Business Vision](vision/business-plan.md), [Technical Plan](vision/tech-plan.md) and [Roadmap](ROADMAP.md).

## Integrated on main

- Main at snapshot: `df71ef1f39ec0c8569c863e4185ae870b3db59bf`.
- PRs #146, #150, #149, #152, #148, #193 and #194 are merged. #148 adds deterministic AI timeout, caller-cancellation and output-bound tests; it is not independent AI safety acceptance.
- PR #194: migration 030, tenant/generation-scoped immutable analytics facts and quarantine, per-tenant active-generation pointer, bounded live analytics worker, active-generation-only summaries with pending-event readiness, production image and optional Compose profile. PostgreSQL integration tests cover restart, generation mismatch, idempotency, shadow isolation and pointer cutover; CI/E2E/Security/CodeQL/Agent Guard passed on exact head.
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
- #133 remains blocked and must be reworked before merge. PR #194 supersedes its original migration 025, facts schema, projector/summary package and runtime-population gap. Do not reintroduce `025_analytics_event_projection.sql` or duplicate those files. Reconcile #133 into an API-route/contracts/spec-only follow-up against current main; preserve compatible `mvp_http.go`, `policy.go`, OpenAPI and tests only after checking current ownership and adapting tests to migration 030.
- PR #194 provides live polling without a monotonically advancing ID watermark: query bounded outbox rows absent from the generation-specific `processed_events`, then call transactional `Consumer.Process`. Keep this pattern; PostgreSQL sequence IDs can commit out of order.
- Historical generation rebuild is still NOT complete. Do not manually switch active pointers or reuse generation IDs. A rebuild still needs a defined quiescent snapshot, sequence-safe live-tail catch-up, lag/count validation, failure rollback and an atomic cutover procedure. Keep AN-001/AN-002 `passes:false` until that and independent acceptance exist.
- #137: canonical cancellation-event compatibility. Preserve legacy payloads for existing event types; add canonical data additively or under a versioned event contract, with old/new contract tests and a recorded consumer migration plan. Repository docs identify only a generic optional HTTPS sink; no actual sink/owner/contract is recorded.
- B2B billing waits for independent Order/Ledger gates (#57/#10) and canonical event readiness (#87).
- No retention duration, privacy policy, fairness threshold, tax/legal rule or regulated-provider approval is invented.
- GitHub security settings require owner-side confirmation; AI affecting money, safety, identity, access or reputation remains advisory until deterministic policy and/or authorized human approval.
- Fake adapters are for local/CI only; no real payment capture, KYC or production messaging is implied.

## Acceptance and execution rules

- feature_list.json is the structured 93-feature inventory; as of 2026-11-25, 0/93 are independently QA-passing. This is an acceptance-evidence count, not an implementation count. Do not set passes=true from CI alone.
- Keep module ownership and public API/event boundaries; use small independent PRs and work on other tasks while CI runs.
- Required checks must pass on current PR head and current main base. Do not bypass branch protection, security gates or independent QA.
- progress.md records evidence; roadmap records intended outcomes. Neither substitutes for issue-level acceptance.
- Local GitHub CLI access from this runtime was unavailable (DNS); GitHub-hosted CI is the execution evidence. No local test success is claimed.
