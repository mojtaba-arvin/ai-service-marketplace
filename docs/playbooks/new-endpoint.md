# Add an API Endpoint

## When
A module needs a new HTTP capability.

## Steps
1. Confirm the owning module and read its specification and ADRs.
2. Define or update the OpenAPI contract first.
3. Specify authentication, authorization, validation, pagination, idempotency, and error semantics.
4. Add acceptance tests for success, invalid input, unauthorized access, forbidden access, and relevant failure paths.
5. Implement transport mapping separately from domain behavior; call only the owning module's public application interface.
6. Add observability without logging secrets or sensitive payloads.
7. Regenerate clients/contracts and document compatibility impact.

## Verify
Run `make verify`; run `make verify-full` for database, queue, or external-adapter behavior.
