# Integration and release acceptance matrix

This matrix maps the release QA scenarios to executable repository evidence. It is a test plan and evidence index, not an maintainer verification sign-off. A green CI run proves only the checks in that run; it does not prove public deployment, real provider configuration, or maintainer acceptance.

## Automated evidence map

| Area | Scenario | Repository evidence | Remaining acceptance |
|---|---|---|---|
| API integration | Health and API contract | `/healthz`, `packages/contracts/openapi.v1.json` | Run against the exact deployed SHA |
| Browser E2E | Customer booking through professional acceptance | `scripts/e2e-browser.sh`, Playwright configuration and CI | Independent execution against clean deployment |
| PostgreSQL integration | Migrations, transaction rollback, replay and recovery | `apps/api/migrations`, migration integration tests, CI backup/restore rehearsal | Restore against actual deployment storage |
| Cross-tenant security | Order, dispatch, identity, organization and analytics isolation | Marketplace PostgreSQL integration tests and negative-path suites | maintainer review of authorization boundaries |
| Concurrency | Refresh rotation, scheduled-order idempotency, dispatch acceptance and ledger posting | PostgreSQL concurrency tests, including PRs #114, #115, #116 and #117 | Required checks on exact release SHA |
| Contract compatibility | HTTP/OpenAPI and versioned domain-event envelopes | `packages/contracts/openapi.v1.json`, `schemas/domain-event.schema.json`, contract validators | External consumer confirmation for additive scheduled-order event delivery |
| AI adversarial behavior | Intake injection and output-contract abuse | `internal/aiintake`, `internal/aieval`, versioned golden dataset and CI | Independent safety review; no consequential AI authority |
| Financial invariants | Balanced immutable ledger and idempotent posting | Ledger PostgreSQL tests and append-only migration constraints | maintainer review of automated finance evidence; no production money movement |
| Deployment | Compose validation, image builds, health check and HTTPS ingress | `infra/compose.production.yml`, `infra/DEPLOYMENT.md`, `scripts/smoke-deployment.sh` | Host, DNS, real secrets/OTP adapter and public smoke run |
| maintainer acceptance | Complete release gate | Issue #57 | Must be completed by an independent verifier; do not self-certify |

## Deployment smoke test

Run after deploying the reviewed build:

```sh
BASE_URL=https://market.example.com \
HTTP_BASE_URL=http://market.example.com \
  ./scripts/smoke-deployment.sh
```

`BASE_URL` must be the HTTPS origin. `HTTP_BASE_URL` is optional and, when supplied, must redirect to HTTPS with status 301 or 308. The script checks API health, an optional, well-formed request correlation ID when the API provides one, and the web origin, and optional HTTP-to-HTTPS redirection. It does not sign in, create orders, or expose test-only OTP endpoints.

## Evidence recording rules

1. Record the exact Git commit SHA, workflow run URLs, environment, timestamp, and test result.
2. Distinguish automated CI from a human/independent verifier and from public-host validation.
3. Do not change `feature_list.json` `passes` fields based solely on implementation or ordinary CI.
4. A missing external consumer approval, host credential, provider secret, or policy decision remains an explicit release gate; do not synthesize evidence.


## Project acceptance policy

No separate independent QA operator, acceptance worker, or external sign-off is required. The project maintainer accepts work after reviewing implementation, automated tests/evaluations, exact-head required CI/security checks, and recorded evidence. Technical gates remain mandatory; merge alone is not acceptance.
