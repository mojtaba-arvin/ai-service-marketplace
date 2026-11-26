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

Run go test ./internal/adapters from apps/api. The tests use deterministic local fakes and do not require credentials or network access.


## Geocoding

The `Geocoder` interface exposes a stable `AdapterID` and accepts a bounded market/locale request with a stable idempotency key. Callers must resolve that exact adapter and the `geocode` capability through the version-bound market matrix before invoking it. Results are rejected when formatted address metadata or coordinate bounds are invalid. `FakeGeocoder` provides deterministic, offline contract tests; it is not a production provider.

## External identity verification

`IdentityTokenVerifier` is the provider-neutral assertion boundary. `JWTIdentityVerifier` verifies configured Apple, Google, or email-provider JWTs using an injected `SigningKeyResolver`; it checks the configured issuer/audience, signature algorithm and key ID, signature, expiry, issued-at, optional not-before, nonce, and authorized party for multi-audience tokens. Email providers that do not use this JWT profile can supply a separate implementation of the same interface.

The key resolver owns provider signing-key discovery and rotation. No live JWKS client, provider credentials, or network-dependent test behavior is included. Production wiring must use trusted provider-specific issuer, audience, algorithm and signing-key configuration. The identity module consumes the verified subject only after market capability checks and one-time nonce consumption. Nonce/link persistence requires the CMD-1754-owned shared tables proposed in issue #232.
