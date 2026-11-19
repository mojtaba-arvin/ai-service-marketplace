# ADR 0002: Modular Monolith for the Transactional Core

- Status: Accepted
- Date: 2026-11-19

## Context
The product has many business domains, but early distributed deployment would add network, serialization, observability, and operational overhead before independent scaling requirements are known.

## Decision
Start with a modular monolith. Each domain owns its internal model and exposes only documented application interfaces and versioned events. Enforce boundaries in code review and CI. Keep AI inference in a separate worker process because its runtime and scaling profile differ materially from transactional requests.

## Consequences
- Simpler local development and atomic database transactions.
- Explicit module boundaries preserve future extraction options.
- Shared database use requires strict ownership and migration discipline.
- A module may be extracted only with evidence of independent scaling, isolation, or deployment needs.

## Alternatives considered
- Microservices from day one: rejected due to premature operational complexity.
- Unstructured monolith: rejected because internal coupling would make later change unsafe.
