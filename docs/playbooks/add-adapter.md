# Add an External Provider Adapter

## When
Integrating a payment, SMS, map, KYC, storage, or notification provider.

## Steps
1. Define a provider-neutral interface and explicit capability/unsupported behavior.
2. Define timeout, retry, rate-limit, idempotency, and error mapping semantics.
3. Implement a deterministic local fake.
4. Add provider contract tests and redact sensitive data from logs.
5. Store credentials only in environment/secret management; never commit them.
6. Make webhook verification and replay protection explicit where applicable.
7. Document market availability and operational fallback behavior.

## Verify
Run unit and adapter contract tests with no live credentials; run `make verify-full` for integration behavior.
