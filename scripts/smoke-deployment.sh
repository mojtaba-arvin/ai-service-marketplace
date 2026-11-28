#!/usr/bin/env bash
set -Eeuo pipefail

BASE_URL="${BASE_URL:?Set BASE_URL to the deployed HTTPS origin}"
case "$BASE_URL" in
  https://*) ;;
  *) echo "BASE_URL must use HTTPS" >&2; exit 2 ;;
esac
BASE_URL="${BASE_URL%/}"
CURL="${CURL:-curl}"
TMP_DIR="$(mktemp -d)"
trap 'rm -rf "$TMP_DIR"' EXIT

command -v "$CURL" >/dev/null

echo "==> Checking API process liveness"
status="$("$CURL" --silent --show-error --output "$TMP_DIR/health.json" \
  --dump-header "$TMP_DIR/health.headers" --connect-timeout 5 --max-time 15 --write-out '%{http_code}' \
  "$BASE_URL/healthz")"
if [[ "$status" != "200" ]] || ! grep -Eq '"status"[[:space:]]*:[[:space:]]*"ok"' "$TMP_DIR/health.json"; then
  echo "liveness endpoint did not return HTTP 200 and status=ok (HTTP $status)" >&2
  exit 1
fi

request_id_header="$(tr -d '\r' <"$TMP_DIR/health.headers" | grep -i '^X-Request-ID:' || true)"
if [[ -n "$request_id_header" ]] && ! grep -Eiq '^X-Request-ID:[[:space:]]*[0-9a-f]{32}[[:space:]]*$' <<<"$request_id_header"; then
  echo "health response contains a malformed X-Request-ID" >&2
  exit 1
fi

echo "==> Checking database-backed API readiness"
ready_status="$("$CURL" --silent --show-error --output "$TMP_DIR/ready.json" \
  --connect-timeout 5 --max-time 15 --write-out '%{http_code}' "$BASE_URL/readyz")"
if [[ "$ready_status" != "200" ]] || ! grep -Eq '"status"[[:space:]]*:[[:space:]]*"ready"' "$TMP_DIR/ready.json"; then
  echo "readiness endpoint did not confirm database readiness (HTTP $ready_status)" >&2
  exit 1
fi

echo "==> Checking deployed web origin"
"$CURL" --fail --silent --show-error --connect-timeout 5 --max-time 15 --output /dev/null "$BASE_URL/"

if [[ -n "${HTTP_BASE_URL:-}" ]]; then
  case "$HTTP_BASE_URL" in
    http://*) ;;
    *) echo "HTTP_BASE_URL must use http://" >&2; exit 2 ;;
  esac
  redirect="$("$CURL" --silent --show-error --output /dev/null --max-time 15 \
    --write-out '%{http_code} %{redirect_url}' "${HTTP_BASE_URL%/}/healthz")"
  code="${redirect%% *}"
  destination="${redirect#* }"
  if [[ "$code" != "301" && "$code" != "308" ]] || [[ "$destination" != https://* ]]; then
    echo "HTTP ingress did not redirect to HTTPS (received: $redirect)" >&2
    exit 1
  fi
fi

echo "Deployment smoke test passed for $BASE_URL"
