# Development Progress

> Rolling evidence snapshot: 2026-11-23. Git history and PRs retain detailed history. Never mark a feature QA-passing without independent evidence.

## Product direction

AI Service Marketplace is a complete open-source, self-hostable, multi-market home-services platform spanning M1–M14. It is not an MVP, demo-only product or single vertical slice. See [README](../README.md), [Product Scope](product-scope.md), [Business Vision](vision/business-plan.md), [Technical Plan](vision/tech-plan.md) and [Roadmap](ROADMAP.md).

## Integrated on main

- Go/PostgreSQL modular-monolith API; Next.js customer web; React/Vite operator UI; Flutter mobile direction; isolated Python AI workloads.
- Identity/tenant memberships, customer order workflows, professional profiles/eligibility, scheduling, availability, dispatch offers, cancellation, order history and transactional outbox.
- Immutable ledger primitives, payment adapter boundaries, market configuration/localization, provider eligibility and B2B organization/property foundations.
- Migration runner, container/Compose deployment, Caddy ingress, browser E2E, PostgreSQL backup/restore rehearsal, CI, CodeQL, Dependency Review and Agent Guard.
- PR #156: full fourteen-domain product definition, adopter-facing README, business/technical vision and dependency-aware roadmap.
- PR #159: grounded Professional Copilot boundary with redaction, retrieval-only citations, strict schema/provenance, mandatory professional review and offline golden evaluation.
- PR #162: platform-only global market configuration mutation, transactional before/after audit snapshots and append-only audit history including TRUNCATE protection.
- PR #168: bounded dynamic service-form schema/answer validator. Persistence/publication, API/UI and order snapshots are separate layers.
- PR #171: initial 77-feature full-product inventory; all entries remain passes=false.
- PR #184: expanded the inventory to 93 features after checking the M1–M14 requirements against existing acceptance criteria; added 14 explicit capabilities for addresses, external identity, live tracking, professional academy/CRM/offline sync, ranked fair dispatch, finance adjustments, background checks, masked voice, operator interventions, consent/retention/secrets Intake Copilot and operator analytics dashboards. No passes value was changed.
- PR #175: session-bound device inventory/revocation; refresh rotation preserves binding. Not hardware attestation or trusted-device proof.
- PR #177: evidence-grounded Dispute Triage advisory boundary with redaction, evidence-cited facts/options, mandatory human adjudication and golden evaluation.

These are repository capabilities, not evidence that every domain is complete or that a production marketplace is publicly deployed.

## Active implementation PRs

- PR #180 merged to main (`164592e51b45e83706d43a84cdd50a98134dac3d`): market/locale-specific service form version lifecycle, platform-operator draft/publish/read APIs, immutable published/retired schemas, serialized version allocation and publish outbox event.
- PR #181: scheduled order answer validation and immutable form-version snapshots; current-head Required Checks are pending. CAT-004/CAT-006 remain passes=false.
- PR #183 merged (`2eada2b6242276c0719e778ab5d77aa743418bb0`): dynamic customer questionnaire rendering and structured answer submission. CAT-004/CAT-006 remain passes=false pending independent acceptance.
- PR #182: correct adopter-facing full-product language and stale MVP framing; Required Checks pending.
- PR #184: 14 missing M1–M14 capabilities decomposed into the feature inventory and linked module issues; current-main Required Checks pending, no acceptance claimed.

## Product workstreams and gates

1. **M1 Identity/privacy:** trusted-device attestation/step-up policy; privacy export/deletion only under approved policy.
2. **M2 Catalog/pricing:** form version lifecycle and order answer snapshots, immutable price rules/snapshots, packages/subscriptions and localized SEO.
3. **M3/M4 Customer/professional:** full intake, scheduling, estimates, tracking, masked communications, completion/variations/reviews/reorder; onboarding/KYC/academy, quotes, earnings, CRM and offline-safe mobile.
4. **M5 Matching:** broadcast/sequential/customer-choice strategies, reassignment, capacity and advisory demand/supply forecasts.
5. **M6 Finance:** escrow/holds, payouts/refunds/chargebacks, tax/invoices/reconciliation; gated on independent Order/Ledger QA.
6. **M7/M8 Trust and communications:** SOS/claims/disputes/moderation/audit, consent-separated channels, protected chat and support copilot.
7. **M9/M10 Admin and growth:** guarded state machine, step-up approval, queues/dashboards, SEO/referrals/campaigns/experiments/lifecycle.
8. **M11/M12 B2B and data:** property-attributed orders/ledger, contracts/webhooks, canonical events, replayable projections and warehouse/dashboards.
9. **M13/M14 Platform and AI:** market/adapters, privacy/DR/observability, Intake/Pro/Dispute/Support/Fraud/Quality/Forecast AI.

## Hard blockers and safety boundaries

- #110: external consumer compatibility for canonical order events; expanded order-state event emission remains gated.
- #133: executable analytics projector and generation semantics; #137: canonical event payload compatibility.
- B2B billing waits for independent Order/Ledger gates (#57/#10) and canonical event readiness (#87).
- No retention duration, privacy policy, fairness threshold, tax/legal rule or regulated-provider approval is invented.
- GitHub security settings require owner-side confirmation; do not change repository security controls without explicit approval.
- AI affecting money, safety, identity, access or reputation remains advisory until deterministic policy and/or authorized human approval.
- Fake adapters are for local/CI only; no real payment capture, KYC or production messaging is implied.

## Acceptance and execution rules

- feature_list.json is the structured 93-feature inventory; all new capabilities stay passes=false until implementation, negative-path tests, exact-head CI and independent acceptance evidence exist.
- Keep module ownership and public API/event boundaries; use small independent PRs and work on other tasks while CI runs.
- Required checks must pass on current PR head and current main base. Do not bypass branch protection, security gates or independent QA.
- progress.md records evidence; roadmap records intended outcomes. Neither substitutes for issue-level acceptance.
- Local GitHub access from this runtime was unavailable (DNS resolution failure); GitHub-hosted CI is the execution evidence. No local test success is claimed.
