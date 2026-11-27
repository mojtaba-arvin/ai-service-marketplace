# Technical Plan — Full-Scale Extensible Marketplace Platform

## Architectural direction

Build the complete product as a modular monolith for transactional domains, with explicit module boundaries, versioned public contracts and extraction paths. One repository and coherent deployment model should enable parallel development without turning every domain into a distributed service. Extract a service only when measured scaling, isolation, ownership or reliability requirements justify an ADR.

The architecture supports all fourteen domains in [Product Scope](../product-scope.md), not only the current customer-to-professional workflow.

## Current implementation baseline and stack decision

The repository already has a Go/PostgreSQL API and migrations, Next.js/TypeScript web client, React/Vite admin surface, Flutter mobile direction and isolated Python AI workloads. This is the current baseline.

The original proposal suggested TypeScript end-to-end (NestJS/Fastify, Drizzle/Prisma, pnpm/Turborepo, Expo), which conflicts with the implementation already integrated here. This plan does not silently rewrite the stack. Go + PostgreSQL remains the default until an explicit ADR compares migration cost, ecosystem benefits, contributor ergonomics, generated contracts and long-term maintenance. No parallel API stack is introduced without an ADR.

## Domain modules and ownership

Modules: identity, catalog, pricing, orders, dispatch, payments/ledger, trust, messaging, support, admin-api, growth, analytics, b2b and platform.

Each module owns its domain invariants, application boundary, persistence/migrations, transport registration, public events, tests, observability and operational docs. Modules interact only through documented public application interfaces or versioned events. Direct imports of another module's internals are prohibited and checked in CI. Cross-module database access must preserve ownership rules.

## Contracts and data

- OpenAPI is source of truth for HTTP APIs; JSON Schema for events and structured payloads.
- Contract changes are versioned and compatibility-checked. Generate clients/DTOs where practical.
- PostgreSQL is transactional source of truth; PostGIS supports service areas and proximity.
- Money uses integer minor units plus ISO currency; never floating point.
- Core entities include users/roles/sessions/devices/addresses, professional profiles/KYC/skills/areas, category/service/form/price rule/plan, order/events/quote/variation/assignment/offer, ledger accounts/entries/holds/payouts/invoices, reviews/disputes/evidence/claims, threads/messages/tickets, outbox events, flags/market config/audit, organizations/properties/memberships and consent/privacy records.
- Retention, export, deletion and audit immutability are explicitly modeled; no retention duration is invented.

## Events and asynchronous work

Outbox writes commit in the same transaction as domain changes. Consumers are idempotent, bounded, observable and tolerate retries/reordering. Event schemas are canonical and versioned. NATS JetStream and Valkey are available choices; introduce/expand them only for documented workloads. Background work includes notification delivery, dispatch timeouts, scheduled plans, reconciliation, analytics projections, AI inference and privacy workflows.

## Order and financial invariants

Order transitions have actor permissions, reasons, append-only history, emitted events and idempotency. Variations require customer approval before scope/price changes. Cancellation/rescheduling is policy-versioned.

Finance is non-negotiable: balanced double-entry postings; integer minor units; idempotency/replay; immutable entries and compensating reversals; explicit escrow/hold lifecycle; PSP callback/timeout reconciliation; daily provider-to-ledger reconciliation; and no real money movement through fake adapters. Finance acceptance requires property-based invariants, concurrency/failure recovery and maintainer verification. B2B billing stays gated until order/property attribution and ledger ownership are verified.

## Adapters

Provider interfaces cover payment, SMS, email, push, maps/geocoding, KYC/background checks, telephony/masked contact, storage/CDN and AI model providers. Each adapter declares capabilities, has deterministic local fakes and contract tests, and documents timeout/retry/idempotency. CI needs no external credentials. Unknown/incomplete market configuration fails closed.

## Realtime and clients

WebSocket/SSE or the selected transport supports dispatch offers, chat, order tracking, ETA and operations updates. Location sharing is consented, purpose-limited and access-controlled. Customer web is SEO-oriented; professional/admin clients expose role-appropriate workflows. Mobile offline support uses explicit local command queues, sync conflict rules and idempotency; money/safety-critical actions cannot be silently finalized offline.

## AI gateway and evaluations

All AI features use an internal gateway with provider abstraction, schema-constrained output, prompt/version tracking, PII redaction, policy checks, cost/time budgets, observability and safe fallback. Golden evaluations cover intake extraction, professional guidance/quote drafting, dispute triage, support assistance, fraud/leakage, quality coaching and demand/ETA/price forecasts.

AI output affecting money, safety, identity, access or reputation is advisory. Deterministic policy and/or authorized human approval is mandatory. Evaluation regression blocks merge; AI cannot directly mutate ledger, KYC, dispute outcome or safety state.

## Security, privacy and operations

Least-privilege RBAC and tenant/organization/property scoping; short-lived credentials and refresh rotation; trusted-device and abuse controls; encryption for sensitive documents; secret manager/KMS; append-only audit; explicit consent/export/deletion and policy-governed retention; OWASP threat modeling; dependency/secret/code scanning; OpenTelemetry-compatible logs/metrics/traces; health/readiness/alerting/runbooks; reproducible backup/restore and adopter-defined RPO/RTO; infrastructure as code. No secrets in Git.

## Verification

- make verify: deterministic formatting/lint/static/unit/contract checks.
- make verify-full: local PostgreSQL and other required infrastructure, integration and E2E smoke checks.
- CI uses fakes rather than live third-party credentials.
- High-risk modules add property-based, race/concurrency, authorization-negative, replay and fault-injection tests.
- A feature passes only after required checks and maintainer acceptance evidence. PR creation or implementation merge alone is not acceptance.

## Deployment and evolution

Start with documented Docker-based self-hosting and a low-operations topology. Keep secrets external, migrations serialized and recovery procedures explicit. Kubernetes is optional and evidence-driven. Real PSP/KYC/tax/background-check activation requires market-specific review and explicit operator action.


## Project acceptance policy

No separate independent QA operator, acceptance worker, or external sign-off is required. The project maintainer accepts work after reviewing implementation, automated tests/evaluations, exact-head required CI/security checks, and recorded evidence. Technical gates remain mandatory; merge alone is not acceptance.
