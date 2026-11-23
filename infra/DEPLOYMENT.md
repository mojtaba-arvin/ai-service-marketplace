# Single-host self-hosted deployment

This is one low-operations deployment topology, not a limitation on the platform's full M1–M14 product scope. It uses: one Go API process, one Next.js web process, one dispatch-expiry worker, PostgreSQL/PostGIS, and Caddy for automatic HTTPS. An optional outbox publisher worker can be enabled when an HTTPS event sink is configured. The default profile does not introduce Kubernetes, Valkey, or NATS.

## Deployment and capability boundary

This Compose profile provides one deployable topology for the currently implemented capabilities. It is not an MVP definition, a restriction of product scope, or evidence that every M1–M14 capability is complete. Use the feature inventory, module specifications, exact-head CI and independent acceptance evidence to assess individual capabilities. Do not expose test OTP/session endpoints or treat ledger primitives as evidence of real payment processing.

Database migrations run in a dedicated one-shot `migrate` Compose service before the API is allowed to start. The runner takes a PostgreSQL advisory lock, applies each migration in its own transaction, and records completed versions in `schema_migrations`. API replicas no longer execute DDL during startup; this removes the single-replica restriction caused by concurrent startup migrations. Keep the migration service serialized and do not run manual DDL concurrently with it.

The web application and API must remain on the same public origin. Caddy routes `/v1/*` to the Go API and all other paths to Next.js. Browser sessions use Secure, HttpOnly access/refresh cookies and a separate same-origin CSRF token. Cookie-authenticated unsafe requests require the CSRF header and reject a supplied cross-origin Origin. Non-browser API clients may continue using the Bearer session transport.

## Host prerequisites

- A Linux host with Docker Engine and the Docker Compose plugin.
- A DNS name whose A/AAAA records point to the host.
- Inbound TCP ports 80 and 443 (and UDP 443 if HTTP/3 is desired).
- A working HTTPS OTP delivery endpoint implementing the adapter contract.
- A strong, unique PostgreSQL password and two independent random peppers (at least 32 bytes each).

PostgreSQL is not published on the host network. Only Caddy publishes HTTP/HTTPS ports.

## Configure secrets

Create an untracked .env.production file in the repository root:

```dotenv
SITE_ADDRESS=market.example.com
POSTGRES_DB=marketplace
POSTGRES_USER=marketplace
POSTGRES_PASSWORD=replace-with-a-strong-unique-password
DATABASE_URL=postgres://marketplace:replace-with-a-strong-unique-password@postgres:5432/marketplace?sslmode=disable
OTP_DELIVERY_URL=https://sms-provider.example.com/v1/messages
OTP_DELIVERY_TOKEN=replace-with-provider-token
OTP_PEPPER=replace-with-at-least-32-random-bytes
SESSION_PEPPER=replace-with-a-different-32-random-bytes
DEFAULT_TENANT_ID=market-om
DEFAULT_MARKET_CODE=OM
BOOTSTRAP_OPERATOR_PHONE=+96890000000
```

Use URL encoding for reserved characters in the PostgreSQL password embedded in DATABASE_URL. Generate peppers on the host, for example with openssl rand -hex 32. Never commit .env.production.

## Seed the initial market catalog and professional

The following example uses Oman values because the repository's existing examples use +968 and OMR. Replace them if the launch market is different. The market code must be a two-letter ISO country code; DEFAULT_TENANT_ID is an internal tenant identifier and must match across the API and seed data.

After the `migrate` service has completed successfully and the API is healthy, load one sample category, price menu, operator identity, and professional profile. The operator phone must exactly match BOOTSTRAP_OPERATOR_PHONE. Both phone numbers must be real E.164 numbers that can receive OTPs.

```sh
set -a
. ./.env.production
set +a
docker compose --env-file .env.production -f infra/compose.production.yml exec -T postgres \
  psql -U "$POSTGRES_USER" -d "$POSTGRES_DB" \
  -v market_code="$DEFAULT_MARKET_CODE" \
  -v tenant_id="$DEFAULT_TENANT_ID" \
  -v locale=en -v currency=OMR -v timezone=Asia/Muscat -v policy_version=sample-policy-v1 \
  -v category_id=sample-home-cleaning -v category_slug=home-cleaning \
  -v category_name="Home cleaning" -v category_description="Standard home cleaning service" \
  -v menu_id=sample-home-cleaning-standard -v service_code=standard-cleaning \
  -v price_version_id=sample-home-cleaning-standard-v1 -v price_minor=2500 \
  -v operator_phone="$BOOTSTRAP_OPERATOR_PHONE" \
  -v professional_phone="+96890000001" -v professional_name="Demo Professional" \
  -v region_code=Muscat -v city_code=Muscat -v area_code=Al-Khuwair \
  -f - < scripts/seed-mvp.sql
```

The sample price is 2.500 OMR in the currency's minor-unit representation. Change the amount, locale, timezone, area, and sample professional phone before using the seed for a different market. The professional remains inactive and unverified until the operator verifies the profile and skill through the operator API.


The OTP delivery adapter sends an HTTPS POST with a bearer token and an idempotency key. Verify the provider request/response contract before enabling sign-in. Missing or invalid delivery configuration must fail startup rather than silently falling back to test authentication.

Outbox events are committed transactionally with their business changes. The default Compose profile persists them in PostgreSQL but does not send them to an external destination. When an HTTPS event sink is available, configure `OUTBOX_PUBLISH_URL`, `OUTBOX_PUBLISH_TOKEN`, and optionally `OUTBOX_WORKER_ID`, then enable the optional worker:

```sh
docker compose --env-file .env.production -f infra/compose.production.yml --profile outbox up -d outbox-worker
docker compose --env-file .env.production -f infra/compose.production.yml logs --tail=100 outbox-worker
```

The publisher sends each event with its database event ID as the `Idempotency-Key`. The sink must deduplicate that key because delivery is at-least-once. Network errors and transient HTTP responses are retried; non-retryable 4xx responses are dead-lettered. Do not enable this profile until the sink contract and idempotency behavior are verified.

## Deploy

Run from the repository root:

```sh
docker compose --env-file .env.production -f infra/compose.production.yml config --quiet
docker compose --env-file .env.production -f infra/compose.production.yml up -d --build
docker compose --env-file .env.production -f infra/compose.production.yml ps
docker compose --env-file .env.production -f infra/compose.production.yml logs --tail=100 migrate api web caddy dispatch-worker
```

Caddy obtains and renews certificates automatically after DNS and firewall configuration are correct. Confirm https://SITE_ADDRESS/healthz returns HTTP 200.

## Backup and restore

Create a logical backup:

```sh
docker compose --env-file .env.production -f infra/compose.production.yml exec -T postgres pg_dump -U "$POSTGRES_USER" -d "$POSTGRES_DB" -Fc > marketplace-$(date -u +%Y%m%dT%H%M%SZ).dump
```

Restore into a separately created empty database after stopping API/worker writes:

```sh
cat marketplace.dump | docker compose --env-file .env.production -f infra/compose.production.yml exec -T postgres pg_restore -U "$POSTGRES_USER" -d "$POSTGRES_DB" --clean --if-exists
```

Test backup restoration before treating the host as production-ready. Keep encrypted backups off-host.

## Known limitations before public use

- Browser sessions use Secure, HttpOnly access/refresh cookies, a same-origin CSRF token, and Origin validation for cookie-authenticated mutations. The browser E2E in open PR #67 passed through local Caddy HTTPS, including CSRF, refresh, access expiry, and sign-out checks; the PR still requires review/merge.
- The customer booking → operator verification/dispatch → professional acceptance → customer status browser journey passed CI in PR #67. A public deployed smoke test is still required.
- CI backup/restore rehearsal against the disposable PostgreSQL service passed in open PR #69; production backup storage and restore must still be rehearsed on the actual host.
- Independent release QA against a clean deployed environment remains required.
- The OTP adapter requires a real provider endpoint and token.
- The initial service catalog and sample professional must be seeded for the configured market before the UI can create a booking.
- Payment capture, escrow movement, refunds, and settlement are not enabled.
- The initial profile supports multiple API replicas after the one-shot migration service completes, but does not include managed database failover.
- Outbox events are persisted but remain unpublished unless the optional outbox profile is enabled with a real HTTPS sink and idempotency support.
- A host, DNS name, OTP credentials, optional outbox sink credentials, and secret values must be supplied by the operator; none are stored in the repository.
