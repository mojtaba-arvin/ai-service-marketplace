#!/usr/bin/env bash
set -Eeuo pipefail

BASE_URL="\${BASE_URL:?Set BASE_URL to the deployed HTTPS origin}"
case "$BASE_URL" in
  https://*) ;;
  *) echo "BASE_URL must use HTTPS" >&2; exit 2 ;;
esac
BASE_URL="\${BASE_URL%/}"
CURL="\${CURL:-curl}"
TMP_DIR="$(mktemp -d)"
trap 'rm -rf "$TMP_DIR"' EXIT

command -v "$CURL" >/dev/null

echo "==> Checking deployed API health"
status="$("$CURL" --silent --show-error --output "$TMP_DIR/health.json" \
  --dump-header "$TMP_DIR/health.headers" --write-out '%{http_code}' \
  "$BASE_URL/healthz")"
if [[ "$status" != "200" ]]; then
  echo "health endpoint returned HTTP $status" >&2
  exit 1
fi
if ! grep -Eq '"status"[[:space:]]*:[[:space:]]*"ok"' "$TMP_DIR/health.json"; then
  echo "health endpoint did not return the expected healthy payload" >&2
  exit 1
fi

request_id_header="$(grep -i '^X-Request-ID:' "$TMP_DIR/health.headers" || true)"
if [[ -n "$request_id_header" ]]; then
  if ! grep -Eiq '^X-Request-ID:[[:space:]]*[0-9a-f]{32}[[:space:]]*$' <<<"$request_id_header"; then
    echo "health response contains a malformed X-Request-ID" >&2
    exit 1
  fi
fi

echo "==> Checking deployed web origin"
"$CURL" --fail --silent --show-error --output /dev/null "$BASE_URL/"

if [[ -n "\${HTTP_BASE_URL:-}" ]]; then
  case "$HTTP_BASE_URL" in
    http://*) ;;
    *) echo "HTTP_BASE_URL must use http://" >&2; exit 2 ;;
  esac
  redirect="$("$CURL" --silent --show-error --output /dev/null --max-time 15 \
    --write-out '%{http_code} %{redirect_url}' "\${HTTP_BASE_URL%/}/healthz")"
  code="\${redirect%% *}"
  destination="\${redirect#* }"
  if [[ "$code" != "301" && "$code" != "308" ]] || [[ "$destination" != https://* ]]; then
    echo "HTTP ingress did not redirect to HTTPS (received: $redirect)" >&2
    exit 1
  fi
fi

echo "Deployment smoke test passed for $BASE_URL"
