# Product Vision — Open-Source Managed Home Services Marketplace

## Project intent

Build an open-source, production-oriented platform for operating a two-sided home-services marketplace that connects households and organizations with independent service professionals.

This repository is a reusable software project and reference implementation. Its purpose is to develop dependable marketplace capabilities, clear domain boundaries, integration contracts, operational tooling, and deployment practices that can be self-hosted, studied, extended, and adapted by other teams.

The project is **not** a market-entry plan, country launch commitment, or authorization to activate regulated services. Geographic information supplied by the project owner is engineering context for realistic configuration, localization, and compatibility testing.

## Product shape

The platform follows a managed-marketplace model. Trust, service quality, transparent pricing, reliable order handling, and auditable operations are core software capabilities.

Primary actors:
- **Customer:** discovers services, requests and schedules work, approves quotes or changes, and follows order progress.
- **Professional:** maintains a profile, skills, service areas, availability, and accepts eligible work.
- **Operator/support:** handles verification workflows, exceptions, disputes, and operational interventions under explicit permissions.
- **Organization:** can later manage multiple properties, members, service requests, and consolidated records.

## Core product capabilities

1. Identity, authentication, sessions, and role-scoped authorization.
2. Service taxonomy, professional skills, service areas, and availability.
3. Versioned pricing, fixed-price services, and customer-approved quotes.
4. Order scheduling, lifecycle transitions, dispatch candidates, expiring offers, and acceptance.
5. Immutable financial records and provider-neutral payment contracts.
6. Trust, verification, support, dispute, and audit workflows.
7. Notifications, transactional events, and reliable background processing.
8. Market configuration, localization, and provider adapters.
9. Operational administration, analytics, and optional organization/B2B workflows.
10. AI-assisted intake and support capabilities with deterministic policy boundaries.

The feature backlog and acceptance evidence—not this vision document—define implementation status.

## Product principles

- Make the core workflows correct, observable, secure, and testable before optimizing for scale.
- Keep the transactional core as a modular monolith until measured evidence justifies extraction.
- Keep module ownership and public interfaces explicit.
- Treat PostgreSQL as the source of truth for transactional state.
- Store money as integer minor units with explicit currency; use immutable, balanced double-entry records for financial postings.
- Make retries and external side effects idempotent; use an outbox for reliable event delivery.
- Enforce authorization server-side and default to least privilege.
- Keep provider-specific behavior behind replaceable adapters with offline fakes and contract tests.
- AI can assist with intake, classification, drafting, and support; it must not autonomously decide identity, safety, reputation, disputes, or payable amounts.
- Preserve privacy through data minimization, redaction, retention controls, and auditable access.

## Geographic and localization context

The owner identified **Armenia and the six GCC countries** as useful regional context:
- Armenia
- Bahrain
- Kuwait
- Oman
- Qatar
- Saudi Arabia
- United Arab Emirates

Use this context to exercise country-aware configuration and identify localization dimensions such as currency, locale, language/direction, timezone, address formats, maps, messaging, and provider capabilities.

This list is not a launch sequence, exclusive product scope, regulatory assessment, or instruction to enable real payments, KYC, tax, or other regulated integrations. Keep examples and fixtures clearly marked as non-production. Country-specific legal or provider requirements must not be inferred from this list.

## Quality attributes

- **Correctness:** explicit state machines, transactional invariants, concurrency safety, and reproducible migrations.
- **Security:** least privilege, secure sessions, abuse controls, secret hygiene, and threat-informed tests.
- **Reliability:** bounded retries, idempotency, health/readiness checks, backups, and recovery procedures.
- **Extensibility:** versioned contracts, modular ownership, adapter boundaries, and documented ADRs.
- **Portability:** reproducible local development and containerized self-hosting without mandatory proprietary services.
- **Accessibility and localization:** responsive interfaces, semantic UI, locale-aware formatting, and support for right-to-left languages where required.
- **Operability:** structured logs, metrics, traces, audit trails, and clear runbooks.

## Non-goals of this document

This document does not define revenue targets, commission rates, customer acquisition, market sizing, launch dates, country prioritization, or legal conclusions. Those topics are outside the engineering vision and must not be treated as project acceptance criteria.
