# M1 — Identity and Access Specification

**Feature IDs:** IDENT-001–IDENT-009  
**Status:** core identity implementation exists; IDENT-008/009 module slices are not integrated on `main`; feature acceptance remains pending.

## Ownership

The Identity module owns phone normalization, OTP challenge issuance/verification, session lifecycle, identity-to-tenant memberships, and role resolution. Other modules receive an `Actor` resolved from a verified session and an explicit tenant/role membership; they must not parse bearer tokens or infer authorization from client-supplied identifiers.

Primary integrated implementation:
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

The integrated HTTP authentication surface includes OTP request/verification, session refresh/revoke, `/v1/auth/me`, and authenticated device registration/list/revocation at `/v1/auth/devices`. Internal module callers use `Store.ResolveActor` / `Store.ListIdentityRoles` and pass an `Actor` into module operations.

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

Relevant integrated tests:
- `identity_test.go`, `sessions_test.go`, `memberships_test.go`
- `http_auth_test.go`, `session_cookies_test.go`, `devices_test.go`
- `mvp_http_test.go` for role-scoped workflow access

Required negative cases include invalid/expired/replayed OTP, exhausted attempts, OTP delivery failure, refresh replay, revoked session, missing role membership, cross-tenant actor, and CSRF/Origin failure. Feature acceptance requires maintainer review of implementation, automated tests, exact-head required CI/security checks, and recorded evidence; merge alone is not acceptance.

## Open gaps

- Define an explicit trusted-device attestation and step-up policy before treating any device as trusted; do not infer trust from a client-provided label.
- Define explicit membership administration and revocation lifecycle.
- Add abuse/rate-limit observability and retention/deletion controls for identity records.
- Keep production OTP provider behavior behind a contract-tested adapter; test OTP capture remains test-only.

## IDENT-008 / IDENT-009 module-private implementation slices (CMD-1722)

The following source-level implementation slices exist on the reserved implementation branches for Issues #269 and #271 (PRs #251 and #254 respectively). Those PRs are closed without being merged; these files are therefore **not part of `main`** and must not be described as integrated application capabilities.

- **Saved addresses:** `marketplace/addresses.go` on the Issue #269 branch defines customer-Actor validation, exact market/locale registry validation, identity + tenant scoped create/list/update/default/delete operations, serialized default selection, and a typed conflict for referenced-address deletion. These operations issue SQL against `customer_addresses`; the shared migration creating that table is not integrated.
- **Geocoding:** `adapters/geocoding.go` on the Issue #269 branch defines a provider-neutral `Geocoder`, bounded request validation, stable idempotency key execution through the adapter runner, result/coordinate validation, and deterministic `FakeGeocoder`. The marketplace caller requires the exact adapter's `geocode` capability in the version-bound market matrix. No production geocoder client or live credentials are supplied.
- **External identity:** `adapters/identity_token.go` and `marketplace/addresses_identity.go` on the Issue #271 branch define provider-neutral verification and linking/login orchestration. The JWT verifier supports configured Apple, Google, and email-provider profiles using an injected `SigningKeyResolver`; it checks issuer, audience, allowed algorithm/key ID, signature, expiry, issued-at, optional not-before, nonce, and authorized party for multi-audience tokens. A non-JWT email proof provider may implement `IdentityTokenVerifier` separately. The key resolver owns signing-key discovery/rotation; no live JWKS client or provider credentials are included. Linking requires an existing authenticated identity, market capability, a verified assertion, and one-time nonce consumption in the same transaction as the unique provider-subject link. Login resolves an existing link only and does not create or merge phone identities.
- **Persistence dependency:** these module-private operations refer to `customer_addresses`, `external_identity_links`, and `external_identity_link_nonces`. The corresponding shared migrations are absent from `main`; the SQL-backed slices cannot be treated as runnable end-to-end integration against the current main schema.

## CMD-1754 shared integration still outstanding

The module-private code above does not complete the cross-module/application integration. The following remain coordinated shared work (tracked by issue #232 and the CMD-1754 integration scope):

- Add and review additive shared migrations for saved addresses, external identity links, and one-time link nonces, including constraints, indexes, and tenant/identity ownership.
- Add OpenAPI schemas and public HTTP route registration for address operations and provider link/login start/callback flows.
- Wire order creation to persist an immutable address snapshot and the required saved-address foreign-key/reference semantics; later address edits must not rewrite historical order addresses.
- Implement assignment-authorized retrieval of the exact order address, with server-side tenant, order, and assignment checks; do not expose customer address-book reads as a substitute.
- Add PostgreSQL integration coverage for migration application, transactional order snapshot behavior, authorization boundaries, concurrent default selection, nonce consumption/replay, and provider-subject uniqueness.
- Verify all of the above through the required full integration workflow before describing IDENT-008/009 as integrated or accepted.

The current `main` migration set ends at `035_private_incident_evidence.sql`; the current OpenAPI contract and `marketplace/http.go` do not register address-book or external-identity provider routes. These are explicit integration gaps, not documentation-only omissions.

## Project acceptance policy

No separate independent QA operator or external sign-off is required. The project maintainer reviews implementation, automated tests/evaluations, exact-head required CI/security checks, and recorded evidence. Technical gates remain mandatory; merge alone is not acceptance.
