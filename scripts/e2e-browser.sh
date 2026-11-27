#!/usr/bin/env bash
set -Eeuo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
API_DIR="$ROOT/apps/api"
WEB_DIR="$ROOT/apps/web"
TMP_DIR="$(mktemp -d)"
API_PID=""
WEB_PID=""
CADDY_NAME="marketplace-e2e-caddy"
CADDY_LOG_PID=""

: "${DATABASE_URL:?DATABASE_URL must point to the disposable E2E PostgreSQL database}"

cleanup() {
  set +e
  docker rm -f "$CADDY_NAME" >/dev/null 2>&1
  [[ -z "$CADDY_LOG_PID" ]] || kill "$CADDY_LOG_PID" 2>/dev/null
  [[ -z "$WEB_PID" ]] || kill "$WEB_PID" 2>/dev/null
  [[ -z "$API_PID" ]] || kill "$API_PID" 2>/dev/null
  [[ -z "$WEB_PID" ]] || wait "$WEB_PID" 2>/dev/null
  [[ -z "$API_PID" ]] || wait "$API_PID" 2>/dev/null
  if [[ -f "$TMP_DIR/api.log" ]]; then
    tail -n 80 "$TMP_DIR/api.log"
  fi
  if [[ -f "$TMP_DIR/web.log" ]]; then
    tail -n 80 "$TMP_DIR/web.log"
  fi
  if [[ -f "$TMP_DIR/caddy.log" ]]; then
    tail -n 80 "$TMP_DIR/caddy.log"
  fi
  rm -rf "$TMP_DIR"
}
trap cleanup EXIT INT TERM

if ! command -v psql >/dev/null; then
  sudo apt-get update -qq
  sudo apt-get install -y postgresql-client
fi
command -v docker >/dev/null
command -v npm >/dev/null
command -v go >/dev/null

echo "==> Applying migrations and seeding isolated ZZ/USD browser-test market"
(
  cd "$API_DIR"
  go run ./cmd/migrate
)
psql "$DATABASE_URL" -v ON_ERROR_STOP=1 \
  -v market_code=ZZ -v locale=en -v currency=USD -v timezone=UTC \
  -v policy_version=e2e-v1 -v category_id=e2e-cleaning \
  -v category_slug=home-cleaning -v category_name="Home Cleaning" \
  -v category_description="Disposable browser E2E service category" \
  -v menu_id=e2e-menu -v service_code=standard-cleaning \
  -v price_version_id=e2e-price-v1 -v price_minor=2500 \
  -v operator_phone=+15550000003 -v tenant_id=tenant-e2e \
  -v professional_phone=+15550000002 -v professional_name="E2E Professional" \
  -v region_code=Muscat -v city_code=Muscat -v area_code=Al-Khuwair \
  -f "$ROOT/scripts/seed-mvp.sql"

echo "==> Bootstrapping explicit operator grants for the disposable browser E2E tenant"
(
  cd "$API_DIR"
  DATABASE_URL="$DATABASE_URL" \
    BOOTSTRAP_TENANT_ID=tenant-e2e \
    BOOTSTRAP_OPERATOR_IDENTITY_ID=mvp-operator-identity \
    BOOTSTRAP_OPERATOR_ACTIONS="order:read,order:assign,provider:verify" \
    BOOTSTRAP_REASON="Disposable browser E2E fixture bootstrap" \
    go run ./cmd/operator-permissions-bootstrap
)

echo "==> Building loopback-only E2E API and OTP capture binary"
(cd "$API_DIR" && go build -o "$TMP_DIR/e2e-server" ./cmd/e2e-server)

echo "==> Starting loopback-only E2E API and OTP capture"
(
  cd "$API_DIR"
  exec env APP_ENV=e2e E2E_ENABLE=1 \
    DATABASE_URL="$DATABASE_URL" \
    OTP_PEPPER="e2e-only-otp-pepper-0123456789abcdef" \
    SESSION_PEPPER="e2e-only-session-pepper-0123456789abcdef" \
    DEFAULT_TENANT_ID=tenant-e2e DEFAULT_MARKET_CODE=ZZ \
    BOOTSTRAP_OPERATOR_PHONE=+15550000003 \
    "$TMP_DIR/e2e-server"
) >"$TMP_DIR/api.log" 2>&1 &
API_PID=$!

for _ in $(seq 1 60); do
  if curl -fsS http://127.0.0.1:8080/healthz >/dev/null 2>&1; then break; fi
  sleep 1
done
curl -fsS http://127.0.0.1:8080/healthz >/dev/null

echo "==> Installing pinned Playwright and building the production Next.js app"
(
  cd "$WEB_DIR"
  npm install --no-save --no-package-lock @playwright/test@1.64.1
  npx playwright install --with-deps chromium
  NEXT_TELEMETRY_DISABLED=1 npm run build
  mkdir -p .next/standalone/.next
  cp -R .next/static .next/standalone/.next/static
  if [[ -d public ]]; then cp -R public .next/standalone/public; fi
)

(
  cd "$WEB_DIR"
  exec env HOSTNAME=127.0.0.1 PORT=3000 node .next/standalone/server.js
) >"$TMP_DIR/web.log" 2>&1 &
WEB_PID=$!

for _ in $(seq 1 60); do
  if curl -fsS http://127.0.0.1:3000 >/dev/null 2>&1; then break; fi
  sleep 1
done
curl -fsS http://127.0.0.1:3000 >/dev/null

cat >"$TMP_DIR/Caddyfile" <<'EOF'
{
  admin off
}

https://localhost:8443 {
  tls internal

  handle /v1/* {
    reverse_proxy 127.0.0.1:8080
  }

  handle /healthz {
    reverse_proxy 127.0.0.1:8080
  }

  handle {
    reverse_proxy 127.0.0.1:3000
  }
}
EOF

echo "==> Starting same-origin HTTPS ingress"
docker run --detach --name "$CADDY_NAME" --network host \
  --volume "$TMP_DIR/Caddyfile:/etc/caddy/Caddyfile:ro" caddy:2-alpine \
  >"$TMP_DIR/caddy-container-id"
docker logs -f "$CADDY_NAME" >"$TMP_DIR/caddy.log" 2>&1 &
CADDY_LOG_PID=$!

for _ in $(seq 1 60); do
  if curl --insecure --fail --silent https://localhost:8443/healthz >/dev/null 2>&1; then break; fi
  sleep 1
done
curl --insecure --fail https://localhost:8443/healthz >/dev/null

echo "==> Running Chromium browser E2E through Caddy HTTPS ingress"
(
  cd "$WEB_DIR"
  E2E_BASE_URL=https://localhost:8443 \
    E2E_OTP_CAPTURE_URL=http://127.0.0.1:8081/__e2e/otp \
    npx playwright test --config=playwright.config.ts
)

kill "$CADDY_LOG_PID" 2>/dev/null || true
echo "Browser E2E passed: real OTP/session persistence, CSRF, refresh, sign-out, booking, dispatch, and professional acceptance."
