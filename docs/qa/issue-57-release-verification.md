# Issue #57 — Core workflow and deployment verification

**Tracking issue:** https://github.com/mojtaba-arvin/ai-service-marketplace/issues/57  
**Evidence snapshot:** 2026-11-28  
**Repository baseline inspected:** `main` at `efd320e45da6438eca91dae0ccd408324979f079`  
**Status:** repository automation is integrated; public release verification remains blocked on adopter-owned infrastructure and credentials. This document is an evidence index and execution plan, not a QA sign-off or a claim that the service is deployed.

## 1. Integrated repository evidence

The following work is already merged. Do not duplicate it in another implementation.

| Area | Merged evidence | What it establishes | What it does not establish |
|---|---|---|---|
| Customer/professional/operator web surfaces | [PR #62](https://github.com/mojtaba-arvin/ai-service-marketplace/pull/62) | Initial role-aware web workspaces, catalog, booking, offers, operator queue, production web image | Live host, real OTP, public availability |
| Browser session security | [PR #64](https://github.com/mojtaba-arvin/ai-service-marketplace/pull/64) | Secure HttpOnly cookies, CSRF and Origin checks, refresh/revocation behavior | Public ingress configuration |
| Production topology | [PR #60](https://github.com/mojtaba-arvin/ai-service-marketplace/pull/60) | Docker Compose, private PostgreSQL, API/web/worker, Caddy, secret-driven configuration | Provisioned host, DNS or deployed image |
| One-shot migrations | [PR #66](https://github.com/mojtaba-arvin/ai-service-marketplace/pull/66) | Advisory lock, per-migration transaction, migration history, startup dependency; PR records CI and Security success on exact head `c7ac8428419374edd5a0ffb33fa9e8170b5cb29a` | Production database migration |
| Migration failure recovery | [PR #68](https://github.com/mojtaba-arvin/ai-service-marketplace/pull/68) | PostgreSQL tests for legacy bootstrap, replay, concurrent invocations, rollback after failed migration and retry | Recovery on the adopter's actual database/volume |
| Browser E2E and backup/restore | [PR #74](https://github.com/mojtaba-arvin/ai-service-marketplace/pull/74) | Production-mode browser journey through same-origin HTTPS/Caddy; cookie, CSRF, refresh and sign-out checks; disposable PostgreSQL backup/restore rehearsal | Real SMS provider, public DNS/host or off-host production restore |
| Deployment documentation | [PR #75](https://github.com/mojtaba-arvin/ai-service-marketplace/pull/75) | README and deployment guidance reconciled with merged foundations | Deployment itself |
| Tenant ownership and negative paths | [PR #86](https://github.com/mojtaba-arvin/ai-service-marketplace/pull/86) | Scheduled-order tenant ownership migration, tenant-scoped operator queue and PostgreSQL cross-tenant regression, active support-agent checks, fake-payment idempotency conflict | Full production tenant-isolation acceptance |

The issue thread also records that the browser journey and CI backup/restore rehearsal passed. These are repository/CI results, not public-host evidence.

## 2. Scenario-to-evidence matrix

| Required scenario | Existing repository evidence | Release evidence still required |
|---|---|---|
| Customer authentication and catalog discovery | `apps/web/e2e/marketplace.spec.ts`; `mvp_http_test.go`; OTP/auth integration tests; production Compose requires an OTP adapter | Sign in through the configured real OTP provider and verify the seeded active market/catalog/price on the deployed build |
| Scheduled order and idempotency | `scheduled_orders_test.go`; `mvp_http_test.go`; same-key replay and changed schedule/area/form payload conflicts | Repeat against deployed PostgreSQL and retain sanitized request/result evidence |
| Professional eligibility and offer acceptance | `dispatch_candidates_test.go`; `dispatch_offers_test.go`; browser journey | Verify the actual market seed, verified/active professional and available capacity; complete one real deployed acceptance |
| Competing/stale offers | `dispatch_offers_test.go`: single winner, losing offer superseded, wrong-provider denial and expiry behavior | Confirm on deployed database with two competing professional sessions; retain both outcomes |
| Customer/operator order state and timeline | `mvp_http_test.go`; `order_history_test.go`; browser journey | Compare customer history/timeline and authorized operator queue for the same deployed order |
| Cross-customer and cross-tenant access | `mvp_http_test.go`; PR #86 tenant queue regression; `policy_test.go`; tenant-scoped migration 023 | Exercise customer, professional and operator identities in two real test tenants; verify no cross-tenant read or mutation |
| Authorization-negative scenarios | `http_auth_test.go`, `dispatch_offers_test.go`, `policy_test.go`, browser cookie/CSRF checks | Confirm deployed wrong-role, wrong-professional, missing-session and CSRF denials |
| Database outage | API startup pings PostgreSQL; `Store.Health` pings the pool and `GET /healthz` returns 503 when unavailable | No dedicated fault-injected outage regression was located for this core release gate. Run a controlled staging outage and verify readiness/health failure, no partial order/offer write, and recovery after PostgreSQL returns |
| Migration failure | PR #68 PostgreSQL integration test verifies DDL/history rollback and repair/retry | Run only in a disposable staging database, never production; retain failed-attempt rollback and successful retry evidence |
| Missing/invalid production secrets and OTP outage | Production entrypoint requires DB/OTP configuration and peppers; OTP request returns unavailable when adapter/pepper is absent; `http_auth_test.go` covers provider delivery failure invalidating the challenge | No real provider credential validation or public OTP delivery is evidenced. Verify missing/invalid settings fail closed in staging, then test one real delivery without exposing OTP codes in logs |

### Important scope distinctions

- PR #74's OTP capture service is E2E-only, loopback-bound and excluded from the production API image. It is not a production OTP provider.
- The deployment seed is an example, not an approved launch-market decision. Its Oman `OM`, `+968`, OMR and Muscat values must not be assumed to be the adopter's production market.
- Migration 023 deliberately leaves legacy orders with ambiguous customer membership unassigned. Do not infer or backfill a tenant.
- CI's disposable PostgreSQL restore is not a production backup, off-host retention or real-host restore rehearsal.
- A successful merge or workflow run is not maintainer acceptance.

## 3. Current SHA and workflow evidence boundary

- Inspected `main` head: `efd320e45da6438eca91dae0ccd408324979f079`.
- The GitHub combined-status query returned no status entries for this SHA; the available commit-workflow lookup is PR-triggered only and returned no runs for this merge commit. Therefore this inspection does **not** certify current exact-head CI or Security success.
- PR #66's body records CI and Security success on its exact implementation head `c7ac8428419374edd5a0ffb33fa9e8170b5cb29a`. That is historical evidence for that PR, not the current `main` SHA.
- PR #281 is merged at the inspected `main` SHA, but its body explicitly says exact-head required CI was not yet observed.
- No deployment URL, host identity, running container image digest, deployment receipt or deployed Git SHA was present in the repository evidence inspected.
- Consequently, deployed SHA and public smoke are **unknown / not run**, not inferred from `main` or CI.

Before acceptance, the maintainer must obtain current CI and Security results for the exact release commit (or run them via the repository's required workflow controls) and record their run URLs and conclusions.

## 4. Owner-only actions — required before public release

These actions require the repository/project owner or adopter with access to the target infrastructure. They cannot be completed by a repository-only implementation.

1. **Choose and provision the target environment.** Select the launch country/market and a Linux host with Docker Engine and Docker Compose. Record provider, region, host identity and an owner-approved DNS name. No provider or host is assumed here.
2. **Configure DNS and firewall.** Point the chosen hostname's A/AAAA records to the host. Permit inbound TCP 80/443; UDP 443 is optional for HTTP/3. Confirm public DNS resolution before starting Caddy.
3. **Create production secrets outside Git.** Supply an untracked `.env.production` or secret-manager values for `SITE_ADDRESS`, PostgreSQL database/user/unique password and `DATABASE_URL`, `OTP_DELIVERY_URL`, `OTP_DELIVERY_TOKEN`, independent `OTP_PEPPER` and `SESSION_PEPPER` (each at least 32 bytes), `DEFAULT_TENANT_ID`, approved `DEFAULT_MARKET_CODE`, and the real E.164 `BOOTSTRAP_OPERATOR_PHONE`. Also configure the evidence encryption key ring, active key ID, allowed media types and private ClamAV endpoint required by production startup. Never send these values through GitHub Issues/PRs or commit them.
4. **Validate the OTP provider.** Confirm the provider accepts the documented HTTPS POST, bearer token and idempotency-key contract. Use a non-production destination/credential first. Confirm provider outage fails closed and failed delivery invalidates the OTP challenge. Keep real OTP codes and tokens out of logs and evidence attachments.
5. **Deploy a pinned source revision.** Check out the exact reviewed release SHA, record it before deployment, run the production Compose config validation and deploy using the documented `infra/DEPLOYMENT.md` procedure. Record API and web image IDs/digests, migration result, container health and timestamp. Do not describe a moving `main` branch as the deployed build.
6. **Seed the approved market.** After migration success and API health, adapt `scripts/seed-mvp.sql` using the selected market's actual country, currency, locale, timezone, service, price, operator and professional values. Use real OTP-capable E.164 identities. Verify the catalog item and price are active; have the operator explicitly verify the professional and skill. The example seed does not activate or verify the professional automatically.
7. **Run deployed smoke and end-to-end checks.** Run `scripts/smoke-deployment.sh` against the real HTTPS origin with `BASE_URL` and, when HTTP redirect is exposed, `HTTP_BASE_URL`. Then execute the full customer → operator → professional → customer journey with real non-production OTP identities. Exercise the listed negative cases and the controlled database outage in staging. Do not expose test OTP endpoints.
8. **Rehearse production backup/restore.** Take a PostgreSQL backup, copy it off-host, restore into a separately created isolated database, and verify representative order/dispatch/outbox records. Preserve evidence-storage bytes and the encryption key ring separately; verify recovery without attaching secrets or plaintext evidence.
9. **Record the release evidence.** Attach sanitized results containing exact Git SHA, image digests, CI/Security run URLs, migration version/result, test scenario IDs/results, public smoke timestamp and HTTP outcomes, backup checksum and restore result, environment identifier (not credentials), and residual limitations.
10. **Maintainer acceptance.** The project maintainer reviews the exact-head checks and recorded deployment/test evidence, then decides whether to update feature acceptance and close Issue #57. No independent QA operator or simulated approval is required.

## 5. Release decision

**Blocked — external owner actions and current exact-head workflow evidence remain outstanding.**

Do not mark this issue complete, set feature acceptance flags, claim public availability, or record a deployed SHA until the corresponding evidence exists. If no demonstration host is provisioned, the public smoke/deployed-SHA rows remain explicitly not applicable/not run; repository CI cannot substitute for them.
