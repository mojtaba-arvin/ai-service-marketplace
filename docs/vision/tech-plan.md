# Technical Plan — Performance- and Cost-Conscious Platform

## Architectural direction
Use a **modular monolith** for the transactional core, with explicit module boundaries and extraction paths. Optimize measured latency, throughput, CPU, memory, IO, and infrastructure cost; avoid premature distributed systems. Correctness, privacy, and safety are hard constraints that performance cannot override.

## Selected baseline stack
- Core API: Go, PostgreSQL driver/SQL tooling, explicit domain/application/transport layers.
- Customer web: Next.js + TypeScript, server rendering and caching for local SEO.
- Admin: React + Vite + TypeScript.
- Mobile: Flutter for customer and professional apps.
- AI workloads: Python worker service, isolated behind an internal AI gateway.
- Primary database: PostgreSQL + PostGIS. Add pgvector only for evaluated retrieval needs.
- Async messaging: transactional outbox plus NATS JetStream when asynchronous delivery is required.
- Ephemeral cache/coordination: Valkey, introduced only for justified workloads.
- Contracts: OpenAPI for HTTP APIs; JSON Schema for events and structured payloads.
- Local infrastructure: Docker Compose; deployment initially via containers and a low-operations reverse proxy. Kubernetes requires evidence.

## Why Go for the core
Go offers predictable deployment, low runtime overhead, efficient concurrency, and a mature backend ecosystem. Using Go for the transactional core avoids introducing Rust and Go simultaneously before there are independently justified hot paths. Python remains isolated to AI/data tasks where its ecosystem materially improves delivery. TypeScript is limited to web clients and tooling.

## Domain modules
Identity; catalog; pricing; orders; dispatch; payments/ledger; trust and safety; messaging; support; admin; growth; analytics; B2B; platform/market configuration.

Modules communicate through public application interfaces and versioned domain events. Direct imports of another module's internals are forbidden. Database ownership and transaction boundaries must be explicit.

## Reliability and data
- PostgreSQL is the source of truth for transactional state.
- Transactional outbox writes domain events atomically with state changes.
- Consumers are idempotent and tolerate redelivery and reordering.
- Order state transitions are explicit, guarded, audited, and tested.
- Financial entries are immutable, double-entry, idempotent, and denominated in integer minor units.
- Corrections use compensating entries; reconciliation is mandatory.
- External providers are accessed through adapters with fakes and contract tests.

## Security and privacy
Least privilege, server-side authorization, secure session handling, rate limits, audit trails, encrypted sensitive documents, data minimization, retention/deletion workflows, dependency and secret scanning, and threat modeling for identity, payments, location, and trust workflows. Never commit credentials or production personal data.

## AI safety and evaluation
Use an internal AI gateway for provider abstraction, structured output validation, redaction, observability, cost controls, and evaluations. Maintain golden datasets for each AI capability. AI may assist intake, support, quote drafting, and dispute triage; it must not independently authorize payment, identity, safety, or reputation decisions. Human approval and deterministic policy gates are mandatory for consequential actions.

## Performance approach
Measure before optimizing. Keep hot paths indexed and bounded; isolate analytics and model inference from synchronous order flows; use pagination and query budgets; throttle location updates; cache only data with clear invalidation semantics. Add load tests and resource budgets as representative workloads become available.

## Verification strategy
- `make verify`: deterministic, offline-friendly formatting/lint/type/unit/contract checks.
- `make verify-full`: adds real local infrastructure, integration tests, and end-to-end smoke tests.
- CI must not depend on live third-party credentials.
- Feature status is changed only after independent verification evidence.

## Market adapters
Market configuration covers country, currency, tax, language/direction, legal policies, working hours, payment, SMS, maps, KYC, and notifications. Provider-specific behavior belongs behind adapter contracts, never scattered market conditionals.

## Release and operations
Use short-lived branches, Conventional Commits, focused PRs, protected main, required CI, dependency/security scanning, release notes, staged rollout, observability, backups, and documented recovery procedures. Production release and market activation require human approval.
