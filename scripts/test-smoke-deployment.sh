#!/usr/bin/env bash
set -Eeuo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
TMP_DIR="$(mktemp -d)"
trap 'rm -rf "$TMP_DIR"' EXIT
FAKE_CURL="$TMP_DIR/fake-curl"

cat >"$FAKE_CURL" <<'FAKE'
#!/usr/bin/env bash
set -Eeuo pipefail
args=("$@")
url="${args[${#args[@]}-1]}"
output=""
headers=""
for ((i=0; i<${#args[@]}; i++)); do
  case "${args[$i]}" in
    --output) output="${args[$((i+1))]}" ;;
    --dump-header) headers="${args[$((i+1))]}" ;;
  esac
done

if [[ "$url" == http://* ]]; then
  printf '%s %s' "${FAKE_REDIRECT_STATUS:-308}" "${FAKE_REDIRECT_URL:-https://example.test/healthz}"
elif [[ "$url" == */readyz ]]; then
  printf '{"status":"%s"}\\n' "${FAKE_READY_BODY:-ready}" >"$output"
  printf '%s' "${FAKE_READY_STATUS:-200}"
elif [[ "$url" == */healthz ]]; then
  printf '{"status":"%s"}\n' "${FAKE_HEALTH_BODY:-ok}" >"$output"
  printf 'HTTP/1.1 %s Fake\r\n' "${FAKE_HEALTH_STATUS:-200}" >"$headers"
  case "${FAKE_REQUEST_ID:-valid}" in
    valid) printf 'X-Request-ID: 0123456789abcdef0123456789abcdef\r\n' >>"$headers" ;;
    malformed) printf 'X-Request-ID: malformed\r\n' >>"$headers" ;;
    missing) ;;
  esac
  printf '%s' "${FAKE_HEALTH_STATUS:-200}"
elif [[ "$url" == https://*/ ]]; then
  if [[ "${FAKE_WEB_STATUS:-200}" -ge 400 ]]; then exit 22; fi
else
  echo "unexpected fake curl URL: $url" >&2
  exit 2
fi
FAKE
chmod +x "$FAKE_CURL"

run_success() {
  env CURL="$FAKE_CURL" BASE_URL="https://example.test" HTTP_BASE_URL="http://example.test" "$@" \
    bash "$ROOT_DIR/scripts/smoke-deployment.sh" >/dev/null
}

expect_failure() {
  if run_success "$@" >/dev/null 2>&1; then
    echo "expected smoke test failure for: $*" >&2
    exit 1
  fi
}

run_success
expect_failure FAKE_HEALTH_STATUS=503
expect_failure FAKE_READY_STATUS=503
expect_failure FAKE_READY_BODY=unready
expect_failure FAKE_HEALTH_BODY=unhealthy
expect_failure FAKE_REQUEST_ID=malformed
expect_failure FAKE_WEB_STATUS=503
expect_failure FAKE_REDIRECT_STATUS=302
expect_failure FAKE_REDIRECT_URL=http://example.test/healthz

if env CURL="$FAKE_CURL" BASE_URL="http://example.test" bash "$ROOT_DIR/scripts/smoke-deployment.sh" >/dev/null 2>&1; then
  echo "expected non-HTTPS BASE_URL to fail" >&2
  exit 1
fi
if env CURL="$FAKE_CURL" BASE_URL="https://example.test" HTTP_BASE_URL="https://example.test" bash "$ROOT_DIR/scripts/smoke-deployment.sh" >/dev/null 2>&1; then
  echo "expected non-HTTP HTTP_BASE_URL to fail" >&2
  exit 1
fi

echo "Deployment smoke test contract checks passed"
