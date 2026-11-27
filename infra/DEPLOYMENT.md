# Single-host self-hosted deployment

This is one low-operations deployment topology, not a limitation on the platform's full M1–M14 product scope. It uses: one Go API process, one Next.js web process, one dispatch-expiry worker, PostgreSQL/PostGIS, and Caddy for automatic HTTPS. An optional outbox publisher worker can be enabled when an HTTPS event sink is configured. The default profile does not introduce Kubernetes, Valkey, or NATS.

## Deployment and capability boundary

This Compose profile provides one deployable topology for the currently implemented capabilities. It is not an MVP definition, a restriction of product scope, or evidence that every M1–M14 capability is complete. Use the feature inventory, module specifications, exact-head CI and maintainer acceptance evidence to assess individual capabilities. Do not expose test OTP/session endpoints or treat ledger primitives as evidence of real payment processing.

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
EVIDENCE_ACTIVE_KEY_ID=2026-11
EVIDENCE_ENCRYPTION_KEYS='{"2026-11":"replace-with-64-hex-characters"}'
EVIDENCE_MAX_BYTES=10485760
EVIDENCE_MAX_PER_REPORT=5
EVIDENCE_ALLOWED_MEDIA_TYPES=application/pdf,image/jpeg,image/png,text/plain
EVIDENCE_CLAMD_ADDRESS=clamav.internal:3310
# Optional; unset means no expiry is applied. Expired evidence is not retrievable.
EVIDENCE_RETENTION_DAYS=
```

Use URL encoding for reserved characters in the PostgreSQL password embedded in DATABASE_URL. Generate peppers on the host, for example with openssl rand -hex 32. Never commit .env.production.

### Private incident evidence

The API requires a private persistent storage volume, an AES-256 key ring and a reachable ClamAV `clamd` service on a private network. Generate a new evidence key with `openssl rand -hex 32`; keep the value in the untracked environment file or a secret manager, never in Git or PostgreSQL. `EVIDENCE_ENCRYPTION_KEYS` is a JSON object keyed by immutable key IDs. Keep every historical key needed to decrypt existing objects when rotating: add the new key, change `EVIDENCE_ACTIVE_KEY_ID`, deploy, then retire an old key only after its objects have been securely re-encrypted and verified. Back up the key ring separately from the encrypted evidence volume and database; loss of a key makes its evidence unrecoverable.

Compose creates a named `evidence_data` volume with owner UID 10001 and mode 0700. The initial adapter stores authenticated-encrypted objects outside the web root with mode 0600. The scanner address must resolve to a trusted private clamd endpoint; scanner timeout, unavailability or a non-OK response rejects uploads. The API does not fall back to unscanned or plaintext storage.

`EVIDENCE_RETENTION_DAYS` is optional and has no default. When explicitly set, the API denies retrieval after that interval. It does not automatically delete bytes. The metadata schema includes a legal-hold flag, but an audited retention/deletion administration workflow and backup-expiry coordination are not yet delivered; do not claim retention deletion or legal-hold operations are operational. Until that workflow is accepted, preserve evidence and backups according to the adopter's approved policy.

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
  -v category_id=mvp-home-cleaning -v category_slug=home-cleaning \
  -v category_name="Home cleaning" -v category_description="Standard home cleaning service" \
  -v menu_id=mvp-home-cleaning-standard -v service_code=standard-cleaning \
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
python3 scripts/validate-deployment-config.py .env.production
docker compose --env-file .env.production -f infra/compose.production.yml config --quiet
docker compose --env-file .env.production -f infra/compose.production.yml up -d --build
docker compose --env-file .env.production -f infra/compose.production.yml ps
docker compose --env-file .env.production -f infra/compose.production.yml logs --tail=100 migrate api web caddy dispatch-worker
```

Caddy obtains and renews certificates automatically after DNS and firewall configuration are correct. Verify both process liveness and database readiness, then run the public smoke check:

```sh
BASE_URL="https://$(grep '^SITE_ADDRESS=' .env.production | cut -d= -f2-)" \\
  HTTP_BASE_URL="http://$(grep '^SITE_ADDRESS=' .env.production | cut -d= -f2-)" \\
  bash scripts/smoke-deployment.sh
```

- `/healthz` is process liveness: HTTP 200 means the API process can answer requests. It does not assert PostgreSQL availability.
- `/readyz` is dependency readiness: HTTP 200 with `{"status":"ready"}` means PostgreSQL is reachable; HTTP 503 means the instance must not receive traffic.
- Docker's API healthcheck and the deployment smoke test use `/readyz`. Caddy exposes both endpoints on the public origin; do not use liveness alone as a deployment-success signal.

## Application rollback

Record the currently deployed Git SHA (or immutable release tag), API/Web image IDs, migration version, and a fresh off-host database backup before each release. Keep the previous source revision available locally and do not overwrite or delete its release tag.

For a failed application release whose migrations are backward-compatible with the previous application version:

```sh
# Run in the deployment checkout. Replace with the exact previously deployed SHA.
git fetch --tags origin
git checkout <previous-release-sha>
python3 scripts/validate-deployment-config.py .env.production
docker compose --env-file .env.production -f infra/compose.production.yml config --quiet
docker compose --env-file .env.production -f infra/compose.production.yml build api web
docker compose --env-file .env.production -f infra/compose.production.yml up -d --no-deps api web dispatch-worker
docker compose --env-file .env.production -f infra/compose.production.yml ps
```

Run `scripts/smoke-deployment.sh` again and record the restored SHA. Keep the current database and all migration records: **do not run down migrations or delete migration history**. The migration runner intentionally applies forward-only, transactional migrations; application rollback is safe only while the previous application remains compatible with the expanded schema.

If a migration is not backward-compatible or data was corrupted, stop API and worker writes, preserve the failed database for investigation, and restore the pre-deploy backup into a separately verified database before switching `DATABASE_URL` back to it. Restore the matching encrypted evidence volume and its separately stored evidence key ring as well; a PostgreSQL dump alone cannot restore private evidence objects. Verify the restored database and evidence before restarting API/workers. Never overwrite the only backup or attempt an untested in-place schema downgrade.

After rollback, leave the failed release and its logs available for diagnosis. A rollback is not complete until the HTTPS smoke check, application journey checks, and data-integrity checks pass.

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

## Analytics projection operations

The optional `analytics` Compose profile is disabled by default. The live worker consumes only the configured tenant's active generation; the one-shot `/app/analytics-rebuild` command performs historical replay, validation and atomic cutover. See [the analytics worker and rebuild runbook](../apps/api/internal/analytics/worker.md) for implementation semantics.

### Production rebuild change procedure

Do not run a rebuild as part of ordinary deployment or without an approved change window. Before starting:

1. Record the exact deployed API image/release SHA and the target tenant.
2. Take a fresh PostgreSQL backup, copy it off-host, and verify that it restores into an isolated empty database.
3. Choose a fresh generation ID that has never been active. Record the currently active generation as the rollback target.
4. Confirm the database has sufficient disk/IO headroom and arrange monitoring for PostgreSQL load, worker logs, pending-event counts, fact/quarantine counts and API analytics readiness.
5. Do not delete or reuse the old generation. A nonzero quarantine count is accounted for but those events do not become metric facts.

Stop the currently configured analytics worker so an old-generation process cannot remain running after cutover:

```sh
docker compose --env-file .env.production -f infra/compose.production.yml --profile analytics stop analytics-worker
```

Set `ANALYTICS_GENERATION_ID` to the new generation and `ANALYTICS_REBUILD_CONFIRM` to the exact `tenant-id:generation-id` pair in the untracked production environment file. Then build the API image and run the one-shot rebuild:

```sh
docker compose --env-file .env.production -f infra/compose.production.yml build api
docker compose --env-file .env.production -f infra/compose.production.yml --profile analytics run --rm --no-deps --entrypoint /app/analytics-rebuild analytics-worker
```

A successful command means the generation passed its source/processed/fact/quarantine checks and its active pointer was switched atomically. It does not prove event-time finality, finance close or independent metric acceptance. Recreate the live worker with the same new generation configuration:

```sh
docker compose --env-file .env.production -f infra/compose.production.yml --profile analytics up -d --no-deps --force-recreate analytics-worker
docker compose --env-file .env.production -f infra/compose.production.yml logs --tail=100 analytics-worker
```

Observe that the worker starts with the new generation, pending events drain, analytics summary readiness recovers, and no unexplained quarantine or database-load regression occurs. Record before/after counts, checksum, timestamps, deployed SHA and operator in the change record. Events committed after the rebuild's cutover snapshot are expected to be consumed by this restarted worker.

### Rollback

If the new worker handoff or post-cutover checks fail, stop it. Set `ANALYTICS_GENERATION_ID` to the retained prior generation, set `ANALYTICS_REBUILD_MODE=rollback`, and set the confirmation to the same `tenant-id:generation-id` pair. Run the one-shot command above. It catches up and validates the retained generation before atomically restoring its pointer. Recreate the worker using that restored generation ID, then clear `ANALYTICS_REBUILD_MODE`. Preserve both generations and the backup until recovery is accepted. Never manually update generation tables or the active pointer.

## Known limitations before public use

- Browser sessions use Secure, HttpOnly access/refresh cookies, a same-origin CSRF token, and Origin validation for cookie-authenticated mutations. Production-mode browser/API E2E and the clean-database PostgreSQL backup/restore rehearsal are merged in PR #74; these prove repository/CI behavior, not a public deployment.
- The customer booking → operator verification/dispatch → professional acceptance → customer status browser journey is covered by the merged production-mode E2E. A public deployed smoke test is still required.
- CI backup/restore rehearsal against a disposable PostgreSQL service is merged in PR #74; production backup storage, off-host retention and restore must still be rehearsed on the actual host.
- Independent release QA against a clean deployed environment remains required.
- The OTP adapter requires a real provider endpoint and token.
- The initial service catalog and sample professional must be seeded for the configured market before the UI can create a booking.
- Payment capture, escrow movement, refunds, and settlement are not enabled.
- The initial profile supports multiple API replicas after the one-shot migration service completes, but does not include managed database failover.
- Outbox events are persisted but remain unpublished unless the optional outbox profile is enabled with a real HTTPS sink and idempotency support.
- A host, DNS name, OTP credentials, optional outbox sink credentials, and secret values must be supplied by the operator; none are stored in the repository.


## Project acceptance policy

No separate independent QA operator, acceptance worker, or external sign-off is required. The project maintainer accepts work after reviewing implementation, automated tests/evaluations, exact-head required CI/security checks, and recorded evidence. Technical gates remain mandatory; merge alone is not acceptance.
