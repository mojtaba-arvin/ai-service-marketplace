# Complete Product Capability Catalogue

This is the adopter-facing product scope. It complements module specifications and the roadmap; it does not assert implementation or QA completion. Every capability must be decomposed into independently testable S/M features and linked to issues.

## M1 — Identity and accounts
- OTP mobile registration/login with replaceable delivery adapters; optional email, Apple and Google identity providers by market.
- Customer, professional, administrator, support, finance and building-manager roles with fine-grained server-side RBAC.
- Profiles, multiple addresses with map/geocoding adapter, registered/trusted devices and session inventory/revocation/rotation.
- Rate limits, abuse controls and security events.
- Consent, privacy access/export, deletion/anonymization and policy-governed retention. Never invent retention periods.

## M2 — Catalog and service structure
- Hierarchical admin-defined categories/services without code changes.
- Versioned dynamic questionnaires/forms with conditional fields, validation and media evidence.
- Versioned price menus, multipliers, rules and immutable order price snapshots.
- Packages and recurring subscriptions with pause/cancel/billing lifecycle.
- Localized service×region SEO content; AI drafts/reviews but publication is permissioned.

## M3 — Customer experience
- Intake by conversation, structured form and photo; AI drafts structured requests and necessary clarifying questions.
- Availability slots, urgency and rescheduling.
- Explained fixed price or estimate range with assumptions/provenance.
- Live professional location, map and ETA with consent/access controls.
- Masked chat/call adapter; raw contact details hidden by default.
- Completion approval, customer-approved variations, invoice access, bilateral reviews with anti-abuse, order history/reorder and equipment history.
- Transparent versioned cancellation/rescheduling policy.

## M4 — Professional workspace
- Multi-step onboarding, KYC, credential evidence, skill tests and academy content.
- Calendar, availability, service areas and supported services.
- Realtime offers via push/SMS, accept/decline and concurrency-safe reservation.
- Structured quote for unknown work with customer approval.
- Earnings/wallet, settlement statements, invoices and future credit adapter boundary; lending disabled by default.
- Lightweight CRM, invoicing, income reports and work history.
- Offline-first critical workflow support with explicit sync/idempotency; no unsafe offline money/safety finalization.
- Quality dashboard, explainable scoring and coaching.

## M5 — Matching, dispatch and capacity
- Explainable eligibility/ranking by distance, skills, rating, availability and configurable fair distribution.
- Broadcast, sequential-offer and customer-choice strategies.
- Reassignment on cancellation, timeout or delay with bounded retries.
- Demand prediction and regional supply-shortage alerts.
- Operator controls to open/close markets, regions and services with audit/effective dates.

## M6 — Payments, ledger and finance
- Immutable, idempotent, replay-safe double-entry ledger with integer minor units and balanced journals.
- Escrow/hold/capture/release/refund lifecycle tied to explicit order and approval policy.
- Provider-neutral PSP adapters for cards, wallets and approved credit/installment products.
- Commission, reward, penalty, coupon and credit postings with explicit accounting treatment.
- Professional payout/settlement, refunds, chargebacks and dispute adjustments.
- Invoices, tax reporting and daily PSP-to-ledger reconciliation.
- Explainable payment fraud signals with human review; no autonomous punitive action.

## M7 — Trust and safety
- Identity/credential verification, background-check adapters, blocklists and periodic re-review.
- SOS, trip sharing and emergency contact workflows with explicit escalation ownership.
- Insurance/guarantee claims and evidence lifecycle.
- Disputes with photos/chat/records, AI summary/suggestions and mandatory human adjudication.
- Explainable suspicious-behavior/leakage signals with privacy controls.
- Content/review moderation and appeals.
- Append-only audit for sensitive actions/configuration.

## M8 — Communications
- Push, SMS, email and optional WhatsApp/Telegram/local channels through adapters, templates and delivery state.
- Separate transactional messages from marketing consent/opt-in/unsubscribe.
- Realtime in-app chat with PII/contact leakage controls.
- Support ticketing, knowledge base and AI suggestions for support agents.

## M9 — Admin and operator console
- Customer, professional, order, region and market management.
- Visible order state machine, guarded manual intervention and reason/audit trail.
- Dispute, finance, trust and support queues with assignment/escalation.
- Pricing, campaigns, policies, feature flags and market activation controls.
- Operations, demand/supply and finance dashboards.
- Fine-grained RBAC and step-up/two-person approval for consequential actions.

## M10 — Growth and marketing
- Dynamic SEO pages by service/region and localized editorial/blog/help content.
- Referral attribution, coupon/campaign management and landing pages.
- Conversion attribution and A/B experiments with hypothesis, metric and guardrails.
- Consent-aware lifecycle email/push with suppression.

## M11 — B2B and subscriptions
- Organization accounts, roles/membership and multiple properties.
- Maintenance contracts, recurring service schedules and approvals.
- Consumption reporting and consolidated invoices/statements.
- Building-manager APIs/webhooks with signed delivery, retries, replay and tenant isolation.

## M12 — Data and analytics
- Canonical versioned event schema, compatibility policy and privacy classification.
- Warehouse/export and idempotent replayable projections.
- Operational, financial, funnel, supply, quality and cohort dashboards.
- Experiment measurement and reproducible analysis.
- Leakage and quality monitoring with explicit source, denominator and time window.

## M13 — Platform and compliance
- Market config for country, language, locale, currency, tax, timezone, RTL, addresses, working hours, policies and provider capabilities.
- i18n and locale-aware formatting across web/admin/mobile/API.
- Replaceable provider adapters and deterministic local fakes.
- Structured logs, metrics, traces, alerting and runbooks.
- Secret management/KMS and rotation procedures.
- Backup, restore and DR with adopter-defined RPO/RTO.
- Consent, privacy export/deletion and policy-controlled retention.

## M14 — AI product capabilities
1. **Intake Copilot:** explanation/photo to structured request, estimate draft and required questions.
2. **Pro Copilot:** troubleshooting guidance, execution checklist and structured quote draft.
3. **Dispute Triage:** case/evidence summary, resolution suggestions and human escalation.
4. **Support Copilot:** response/action suggestions grounded in knowledge and policy.
5. **Leakage/Fraud Detector:** explainable model signals combined with deterministic rules.
6. **Quality Score/coaching:** evidence-based professional feedback with contestability and human oversight.
7. **Demand/ETA/Price Forecast:** forecasts with uncertainty, provenance and operator override.

## Universal AI and safety rules
AI outputs are untrusted proposals. Structured output is schema-validated, versioned, evaluated, redacted, observable and cost-bounded. Any output affecting money, identity, access, safety, dispute outcome or reputation requires deterministic policy checks and/or authorized human approval. Model failure falls back safely; no AI provider is mandatory for core deterministic workflows.

## Universal integration rules
Every external integration has a documented interface, capability declaration, timeout/retry/idempotency semantics, deterministic local fake and contract tests. CI does not require external credentials. Market configuration selects adapters; it does not create country-specific code forks.

## Completion rule
A capability is not complete because it appears here, has a PR or passes a unit test. Completion requires implementation, module contract, relevant migrations, positive/negative tests, risk-appropriate security/privacy review, exact-head CI and independent acceptance evidence in the feature inventory.
