#!/usr/bin/env bash
set -Eeuo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
API_DIR="$ROOT/apps/api"
PORT="${E2E_PORT:-18080}"
export APP_ENV=test
export TEST_OTP_CODE="${TEST_OTP_CODE:-000000}"
export HTTP_ADDR=":$PORT"
: "${DATABASE_URL:?DATABASE_URL must point to the disposable test PostgreSQL database}"

cd "$API_DIR"
go run ./cmd/migrate
go run ./cmd/migrate
go run ./cmd/api >/tmp/marketplace-api-e2e.log 2>&1 &
server_pid=$!
cleanup() {
  kill "$server_pid" 2>/dev/null || true
  wait "$server_pid" 2>/dev/null || true
}
trap cleanup EXIT

base="http://127.0.0.1:$PORT"
ready=0
for _ in $(seq 1 60); do
  if curl -fsS "$base/healthz" >/dev/null; then ready=1; break; fi
  sleep 1
done
if [[ "$ready" != 1 ]]; then
  cat /tmp/marketplace-api-e2e.log >&2
  echo "API did not become healthy" >&2
  exit 1
fi

otp="$(curl -fsS -X POST "$base/v1/test/otp/verify" -H 'Content-Type: application/json' \
  -d '{"phone":"+96890000000","code":"000000"}')"
token="$(printf '%s' "$otp" | jq -r '.access_token')"
[[ "$token" == "test-session" ]]

key="e2e-$(date +%s%N)"
payload='{"amount_minor":1200,"currency":"OMR"}'
first="$(curl -fsS -X POST "$base/v1/orders" -H "Authorization: Bearer $token" \
  -H "Idempotency-Key: $key" -H 'Content-Type: application/json' -d "$payload")"
order_id="$(printf '%s' "$first" | jq -r '.order.id')"
[[ "$(printf '%s' "$first" | jq -r '.created')" == "true" ]]

replay="$(curl -fsS -X POST "$base/v1/orders" -H "Authorization: Bearer $token" \
  -H "Idempotency-Key: $key" -H 'Content-Type: application/json' -d "$payload")"
[[ "$(printf '%s' "$replay" | jq -r '.created')" == "false" ]]
[[ "$(printf '%s' "$replay" | jq -r '.order.id')" == "$order_id" ]]

curl -fsS -X POST "$base/v1/orders/$order_id/transition" \
  -H "Authorization: Bearer $token" -H 'Content-Type: application/json' -d '{"to":"accepted"}' >/dev/null
status="$(curl -sS -o /tmp/marketplace-invalid-transition.json -w '%{http_code}' \
  -X POST "$base/v1/orders/$order_id/transition" -H "Authorization: Bearer $token" \
  -H 'Content-Type: application/json' -d '{"to":"completed"}')"
[[ "$status" == "409" ]]

echo "Walking skeleton E2E passed: migrations (initial + idempotent replay) -> OTP -> idempotent order -> accepted transition -> invalid transition rejected."

echo "==> Starting browser-level production-mode E2E through same-origin HTTPS ingress"
bash "$ROOT/scripts/e2e-browser.sh"
