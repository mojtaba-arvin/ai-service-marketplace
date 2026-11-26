# M1 — Identity and Access Specification

**Feature IDs:** IDENT-001–IDENT-009  
**Status:** implementation exists; independent feature acceptance remains pending.

## Ownership

The Identity module owns phone normalization, OTP challenge issuance/verification, session lifecycle, identity-to-tenant memberships, and role resolution. Other modules receive an `Actor` resolved from a verified session and an explicit tenant/role membership; they must not parse bearer tokens or infer authorization from client-supplied identifiers.

Primary implementation:
- `apps/api/internal/marketplace/identity.go`
- `apps/api/internal/marketplace/sessions.go`
- `apps/api/internal/marketplace/memberships.go`
- `apps/api/internal/marketplace/policy.go`
- `apps/api/internal/marketplace/http.go`
- migrations `002_identity.sql`, `003_sessions.sql`, `022_identity_memberships.sql`

## Data ownership

- `phone_identities`: canonical E.164 identity.
- `otp_challenges`: keyed OTP digest, expiry, attempts, consumption and supersession state.
- `auth_sessions`: opaque access/refresh token digests, expiry, rotation-family and revocation state.
- `identity_memberships`: identity, tenant and role membership. A phone identity can have distinct memberships; a membership is not global authorization.
- `auth_devices`: user-labeled device registrations associated with authenticated sessions; revocation invalidates every session bound to that device.

## Public boundary

The HTTP authentication surface includes OTP request/verification, session refresh/revoke, `/v1/auth/me`, and authenticated device registration/list/revocation at `/v1/auth/devices`. Internal module callers use `Store.ResolveActor` / `Store.ListIdentityRoles` and pass an `Actor` into module operations.

No endpoint may accept a tenant or role from an untrusted request and use it without resolving a matching membership. Test-only OTP endpoints must remain unreachable outside test mode.

## Invariants

1. Normalize phone numbers before persistence; enforce uniqueness on normalized E.164 values.
2. Store OTP and session token digests only; never persist raw credentials.
3. OTP challenges expire, have bounded attempts, and become unusable after success, supersession, delivery failure, or expiry.
4. Refresh rotation invalidates the old token; replay revokes the token family.
5. Role resolution requires an explicit identity + tenant + role membership.
6. Authorization is server-side and deny-by-default; tenant equality is checked before role policy.
7. Cookie-authenticated mutations enforce same-origin CSRF/Origin protections; auth responses are non-cacheable.
8. A device registration can only be attached to the caller's active session and identity; client-supplied identity/device IDs are never trusted.
9. Revoking a device revokes all sessions bound to it; refresh rotation preserves the device binding.
10. Device labels are bounded user-visible metadata. A registration is not, by itself, proof of hardware identity or a step-up authentication policy.

## Events and failure behavior

Authentication failures return generic client-facing errors and must not disclose whether an arbitrary phone identity exists. OTP delivery failures invalidate the challenge. Database/provider failures fail closed; no fallback test token is permitted in production mode.

## Verification

Relevant tests:
- `identity_test.go`, `sessions_test.go`, `memberships_test.go`
- `http_auth_test.go`, `session_cookies_test.go`, `devices_test.go`
- `mvp_http_test.go` for role-scoped workflow access

Required independent negatives: invalid/expired/replayed OTP, exhausted attempts, OTP delivery failure, refresh replay, revoked session, missing role membership, cross-tenant actor, and CSRF/Origin failure. Do not set any IDENT feature to passing until evidence is attached by an independent verifier.

## Open gaps

- Device inventory and session-bound revocation are implemented in the current change; required CI and independent acceptance are pending. Device registration does not claim hardware attestation or trusted-device status.
- Define an explicit trusted-device attestation and step-up policy before treating any device as trusted; do not infer trust from a client-provided label.

- Multi-address profiles resolve through a market-specific geocoding adapter; exact coordinates remain order- and role-scoped.
- Optional email/Apple/Google login is enabled only by market capability configuration; linking requires proof of control of both identities.
- Define explicit membership administration and revocation lifecycle.
- Add abuse/rate-limit observability and retention/deletion controls for identity records.
- Keep production OTP provider behavior behind a contract-tested adapter; test OTP capture remains test-only.
