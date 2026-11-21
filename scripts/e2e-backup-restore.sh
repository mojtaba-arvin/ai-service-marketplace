#!/usr/bin/env bash
set -Eeuo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
TMP_DIR="$(mktemp -d)"
RESTORE_DB="marketplace_restore_$(date +%s%N)"
RESTORE_CREATED=0

: "${DATABASE_URL:?DATABASE_URL must point to the disposable E2E PostgreSQL database}"

cleanup() {
  set +e
  if [[ "$RESTORE_CREATED" == 1 ]]; then
    dropdb --if-exists --maintenance-db="$MAINTENANCE_DATABASE_URL" "$RESTORE_DB"
  fi
  rm -rf "$TMP_DIR"
}
trap cleanup EXIT INT TERM

for command in psql pg_dump pg_restore createdb dropdb python3; do
  if ! command -v "$command" >/dev/null; then
    sudo apt-get update -qq
    sudo apt-get install -y postgresql-client
    break
  fi
done

readarray -t DATABASE_URLS < <(python3 - "$DATABASE_URL" "$RESTORE_DB" <<'PY'
import sys
from urllib.parse import urlsplit, urlunsplit

source = urlsplit(sys.argv[1])
if source.scheme not in ("postgres", "postgresql") or not source.netloc:
    raise SystemExit("DATABASE_URL must be a PostgreSQL connection URI")
maintenance = urlunsplit((source.scheme, source.netloc, "/postgres", source.query, ""))
restore = urlunsplit((source.scheme, source.netloc, "/" + sys.argv[2], source.query, ""))
print(maintenance)
print(restore)
PY
)
MAINTENANCE_DATABASE_URL="${DATABASE_URLS[0]}"
RESTORE_DATABASE_URL="${DATABASE_URLS[1]}"

cat >"$TMP_DIR/compose.env" <<'EOF'
SITE_ADDRESS=market.example.test
POSTGRES_DB=marketplace
POSTGRES_USER=marketplace
POSTGRES_PASSWORD=ci-only-password
DATABASE_URL=postgres://marketplace:ci-only-password@postgres:5432/marketplace?sslmode=disable
OTP_DELIVERY_URL=https://otp.example.test/send
OTP_DELIVERY_TOKEN=ci-only-token
OTP_PEPPER=0123456789abcdef0123456789abcdef
SESSION_PEPPER=abcdef0123456789abcdef0123456789
DEFAULT_TENANT_ID=tenant-e2e
DEFAULT_MARKET_CODE=ZZ
BOOTSTRAP_OPERATOR_PHONE=+15550000003
OUTBOX_PUBLISH_URL=https://events.example.test/v1/events
OUTBOX_PUBLISH_TOKEN=ci-only-outbox-token
OUTBOX_WORKER_ID=ci-outbox-worker
EOF

echo "==> Validating default and optional outbox production Compose profiles"
docker compose --env-file "$TMP_DIR/compose.env" -f "$ROOT/infra/compose.production.yml" config --quiet
docker compose --profile outbox --env-file "$TMP_DIR/compose.env" -f "$ROOT/infra/compose.production.yml" config --quiet

echo "==> Verifying transactional outbox events from the browser journey"
order_events="$(psql "$DATABASE_URL" --no-psqlrc --tuples-only --no-align --set=ON_ERROR_STOP=1 \
  --command="SELECT COUNT(*) FROM outbox_events WHERE event_type='order.created'")"
dispatch_events="$(psql "$DATABASE_URL" --no-psqlrc --tuples-only --no-align --set=ON_ERROR_STOP=1 \
  --command="SELECT COUNT(*) FROM outbox_events WHERE event_type='dispatch.offer_created'")"
unpublished_events="$(psql "$DATABASE_URL" --no-psqlrc --tuples-only --no-align --set=ON_ERROR_STOP=1 \
  --command="SELECT COUNT(*) FROM outbox_events WHERE published_at IS NULL AND dead_lettered_at IS NULL")"
[[ "$order_events" -ge 1 ]] || { echo "order.created outbox event was not persisted" >&2; exit 1; }
[[ "$dispatch_events" -ge 1 ]] || { echo "dispatch.offer_created outbox event was not persisted" >&2; exit 1; }
[[ "$unpublished_events" -ge 2 ]] || { echo "expected unconsumed outbox events with the optional publisher disabled" >&2; exit 1; }

echo "==> Creating an isolated restore database"
createdb --maintenance-db="$MAINTENANCE_DATABASE_URL" "$RESTORE_DB"
RESTORE_CREATED=1

echo "==> Taking a custom-format PostgreSQL backup"
pg_dump --format=custom --no-owner --no-acl --dbname="$DATABASE_URL" \
  --file="$TMP_DIR/marketplace.dump"
test -s "$TMP_DIR/marketplace.dump"

echo "==> Restoring backup into a clean database"
pg_restore --no-owner --no-acl --exit-on-error --dbname="$RESTORE_DATABASE_URL" \
  "$TMP_DIR/marketplace.dump"

snapshot() {
  local database_url="$1"
  psql "$database_url" --no-psqlrc --tuples-only --no-align --field-separator='|' \
    --set=ON_ERROR_STOP=1 --command="
      SELECT 'schema_migrations', COUNT(*) FROM schema_migrations
      UNION ALL SELECT 'markets', COUNT(*) FROM markets
      UNION ALL SELECT 'service_categories', COUNT(*) FROM service_categories
      UNION ALL SELECT 'service_price_versions', COUNT(*) FROM service_price_versions
      UNION ALL SELECT 'phone_identities', COUNT(*) FROM phone_identities
      UNION ALL SELECT 'identity_memberships', COUNT(*) FROM identity_memberships
      UNION ALL SELECT 'provider_profiles', COUNT(*) FROM provider_profiles
      UNION ALL SELECT 'orders', COUNT(*) FROM orders
      UNION ALL SELECT 'dispatch_offers', COUNT(*) FROM dispatch_offers
      UNION ALL SELECT 'dispatch_assignments', COUNT(*) FROM dispatch_assignments
      UNION ALL SELECT 'provider_bookings', COUNT(*) FROM provider_bookings
      UNION ALL SELECT 'outbox_events', COUNT(*) FROM outbox_events
      UNION ALL SELECT 'ledger_journals', COUNT(*) FROM ledger_journals
      UNION ALL SELECT 'ledger_entries', COUNT(*) FROM ledger_entries
      UNION ALL SELECT 'constraints', COUNT(*) FROM pg_constraint
        WHERE connamespace = 'public'::regnamespace
      UNION ALL SELECT 'user_triggers', COUNT(*) FROM pg_trigger
        WHERE NOT tgisinternal
          AND tgrelid IN (SELECT oid FROM pg_class WHERE relnamespace = 'public'::regnamespace)
      UNION ALL SELECT 'sequences', COUNT(*) FROM pg_class
        WHERE relkind = 'S' AND relnamespace = 'public'::regnamespace
      ORDER BY 1;
    "
}

echo "==> Comparing schema and persisted business-data counts"
snapshot "$DATABASE_URL" >"$TMP_DIR/source.snapshot"
snapshot "$RESTORE_DATABASE_URL" >"$TMP_DIR/restored.snapshot"
if ! diff -u "$TMP_DIR/source.snapshot" "$TMP_DIR/restored.snapshot"; then
  echo "Backup/restore verification mismatch" >&2
  exit 1
fi

echo "Backup/restore E2E passed: custom dump, clean restore, migration history, schema objects, and MVP data counts match."
