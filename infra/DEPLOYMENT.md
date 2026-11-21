# Single-host MVP deployment

This deployment profile is intentionally small: one Go API process, one Next.js web process, one dispatch-expiry worker, PostgreSQL/PostGIS, and Caddy for automatic HTTPS. It does not introduce Kubernetes, Valkey, or NATS into the first runtime.

## Release boundary

The repository now contains the first customer/professional/operator API and browser workflow. Treat it as an MVP release candidate until the full CI suite, configured OTP delivery, host deployment, and end-to-end acceptance gate have all passed. Do not expose test OTP/session endpoints or treat ledger primitives as evidence of real payment processing.

Database migrations run in a dedicated one-shot `migrate` Compose service before the API is allowed to start. The runner takes a PostgreSQL advisory lock, applies each migration in its own transaction, and records the SHA-256 checksum with each version in `schema_migrations`. Re-running an unchanged migration is a no-op; changing the contents of an already-applied migration is a hard error and must be handled by adding a new forward migration instead. API replicas no longer execute DDL during startup; this removes the single-replica restriction caused by concurrent startup migrations. Keep the migration service serialized and do not run manual DDL concurrently with it.

The web application and API must remain on the same public origin. Caddy routes `/v1/*` to the Go API and all other paths to Next.js. Browser sessions use Secure, HttpOnly access/refresh cookies and a separate same-origin CSRF token. Cookie-authenticated unsafe requests require the CSRF header and reject a supplied cross-origin Origin. Non-browser API clients may continue using the Bearer session transport.

The web application and API must remain on the same public origin. Caddy routes `/v1/*` to the Go API and all other paths to Next.js. Browser sessions use Secure, HttpOnly access/refresh cookies and a same-origin CSRF token; do not split the web and API across unrelated origins without revisiting cookie and CSRF policy.

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

## Seed the initial single-market catalog and professional

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
  -v locale=en -v currency=OMR -v timezone=Asia/Muscat -v policy_version=mvp-1 \
  -v category_id=mvp-home-cleaning -v category_slug=home-cleaning \
  -v category_name="Home cleaning" -v category_description="Standard home cleaning service" \
  -v menu_id=mvp-home-cleaning-standard -v service_code=standard-cleaning \
  -v price_version_id=mvp-home-cleaning-standard-v1 -v price_minor=2500 \
  -v operator_phone="$BOOTSTRAP_OPERATOR_PHONE" \
  -v professional_phone="+96890000001" -v professional_name="Demo Professional" \
  -v region_code=Muscat -v city_code=Muscat -v area_code=Al-Khuwair \
  -f - < scripts/seed-mvp.sql
```

The sample price is 2.500 OMR in the currency's minor-unit representation. Change the amount, locale, timezone, area, and sample professional phone before using the seed for a different market. The professional remains inactive and unverified until the operator verifies the profile and skill through the operator API.


The OTP delivery adapter sends an HTTPS POST with a bearer token and an idempotency key. Verify the provider request/response contract before enabling sign-in. Missing or invalid delivery configuration must fail startup rather than silently falling back to test authentication.

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

- Browser sessions use Secure, HttpOnly access/refresh cookies, a same-origin CSRF token, and Origin validation for cookie-authenticated mutations. Browser-level CSRF/session tests through Caddy are still required before release.
- Browser-level customer booking → professional offer acceptance → customer status visibility E2E is still a release blocker.
- Independent release QA against a clean deployed environment remains required.
- The OTP adapter requires a real provider endpoint and token.
- The initial service catalog and sample professional must be seeded for the configured market before the UI can create a booking.
- Payment capture, escrow movement, refunds, and settlement are not enabled.
- The initial profile supports multiple API replicas after the one-shot migration service completes, but does not include managed database failover.
- A host, DNS name, and secret values must be supplied by the operator; none are stored in the repository.
