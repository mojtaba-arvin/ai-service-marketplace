# External provider adapter framework

This package supplies provider-neutral execution controls. It does not contain credentials, network clients, or vendor-specific behavior.

## Contract

- Every operation requires a stable, non-empty idempotency key. Retries receive exactly the same key; an adapter must enforce deduplication at the provider boundary before retrying side effects.
- Each attempt has a bounded context timeout.
- Retry count is capped at ten and retries are disabled unless the caller explicitly classifies an error as retryable. Permanent validation/authentication errors must not be retried.
- Exponential backoff is bounded by the configured maximum and is interruptible by caller cancellation.
- The optional circuit breaker opens after a configured number of failed logical operations and admits only one half-open probe after its cooldown.
- Provider adapters should expose typed errors so callers can distinguish transient transport failures, permanent rejections, and unknown outcomes.

## Safety boundary

A timeout does not prove that a remote side effect failed. Payment, identity, messaging, and other consequential adapters must reconcile unknown outcomes using the provider's idempotency/query API; they must not blindly create a new operation or key. The runner is not a substitute for signed webhook verification, durable operation state, or business-level authorization.

## Verification

Run `go test ./internal/adapters` from `apps/api`. The tests use deterministic local fakes and do not require credentials or network access.

## Geocoding capability slice (Issue #269)

The Issue #269 implementation branch defines a `Geocoder` interface with a stable `AdapterID`, bounded market/locale request, stable idempotency key, result validation, and deterministic offline `FakeGeocoder`. The marketplace caller must resolve that exact adapter and the `geocode` capability through the version-bound market matrix before invocation. This is a module-private adapter contract, not a production geocoding integration: no live provider client or credentials are included.

The implementation source is on the Issue #269 branch / PR #251, which is closed without merge. It is not present on `main`. Its SQL-backed saved-address caller depends on the shared `customer_addresses` migration, which is also not present on `main`.

## External identity verification capability slice (Issue #271)

The Issue #271 implementation branch defines the provider-neutral `IdentityTokenVerifier` contract and `JWTIdentityVerifier`. The JWT profile accepts only configured providers (Apple, Google, or email), issuer, audience, allowed signing algorithm and key ID; it verifies the signature and temporal/nonce claims, including authorized-party validation for multi-audience tokens. Signing-key discovery and rotation are injected through `SigningKeyResolver`. Email providers that do not use this JWT profile may implement the verifier contract separately.

No live JWKS client, provider credentials, or network-dependent test behavior is included. Production wiring must use trusted provider-specific issuer, audience, algorithm, and signing-key configuration. The identity module consumes the verified subject only after market capability checks and one-time nonce consumption. Nonce/link persistence requires the shared `external_identity_links` and `external_identity_link_nonces` tables.

The implementation source is on the Issue #271 branch / PR #254, which is closed without merge. It is not present on `main`. The module-private SQL operations therefore do not establish an integrated provider flow.

## Shared integration boundary

Neither capability slice adds shared migrations, OpenAPI schemas, HTTP route registration, immutable order-address snapshots/FKs, order-creation wiring, assignment-authorized exact-address retrieval, or the required PostgreSQL end-to-end/concurrency/replay coverage. Those remain CMD-1754 integration work (issue #232). Do not interpret these adapter contracts or their branch-local tests as completion or acceptance of the shared feature.
