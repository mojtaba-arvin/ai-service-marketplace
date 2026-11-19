# Single-host MVP deployment

This deployment profile is intentionally small: one Go API process, one dispatch-expiry worker, PostgreSQL/PostGIS, and Caddy for automatic HTTPS. It does not introduce Kubernetes, Valkey, or NATS into the first runtime.

## Release boundary

This is a deployment foundation, not a claim that the MVP user journey is complete. The current API still needs the production-safe catalog, booking, dispatch, and role-scoped HTTP workflows tracked in [MVP-API](https://github.com/mojtaba-arvin/ai-service-marketplace/issues/54). Do not expose the test OTP/session endpoints or simulated ledger as production behavior.

The API currently applies its versioned SQL migrations during startup. Keep exactly one API replica until migrations are moved to a dedicated one-shot migration job.

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
```

Use URL encoding for reserved characters in the PostgreSQL password embedded in DATABASE_URL. Generate peppers on the host, for example with openssl rand -hex 32. Never commit .env.production.

The OTP delivery adapter sends an HTTPS POST with a bearer token and an idempotency key. Verify the provider request/response contract before enabling sign-in. Missing or invalid delivery configuration must fail startup rather than silently falling back to test authentication.

## Deploy

Run from the repository root:

```sh
docker compose --env-file .env.production -f infra/compose.production.yml config --quiet
docker compose --env-file .env.production -f infra/compose.production.yml up -d --build
docker compose --env-file .env.production -f infra/compose.production.yml ps
docker compose --env-file .env.production -f infra/compose.production.yml logs --tail=100 api caddy dispatch-worker
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

- The full customer/professional/operator HTTP workflow and browser UI are not yet implemented.
- The OTP adapter requires a real provider endpoint and token.
- Payment capture, escrow movement, refunds, and settlement are not enabled.
- The initial profile runs one API replica and does not include managed database failover.
- A host, DNS name, and secret values must be supplied by the operator; none are stored in the repository.
