#!/usr/bin/env bash
set -Eeuo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

FULL=0
if [[ "${1:-}" == "--full" ]]; then
  FULL=1
elif [[ "$#" -gt 0 ]]; then
  echo "Usage: $0 [--full]" >&2
  exit 2
fi

echo "==> Checking whitespace errors"
git diff --check

echo "==> Validating JSON files"
while IFS= read -r -d '' file; do
  python3 -m json.tool "$file" >/dev/null
  echo "    OK $file"
done < <(find . -type f -name '*.json' -not -path './.git/*' -print0)

echo "==> Checking OpenAPI contract invariants"
python3 scripts/check_openapi.py

echo "==> Checking feature-list invariants"
python3 scripts/check_feature_list.py

if [[ -f apps/api/go.mod ]]; then
  echo "==> Running Go tests"
  (cd apps/api && go test ./...)
fi

echo "==> Checking shell syntax"
while IFS= read -r -d '' file; do
  bash -n "$file"
done < <(find scripts -type f -name '*.sh' -print0)

if [[ "$FULL" -eq 1 ]]; then
  echo "==> Validating and starting local infrastructure"
  docker compose config --quiet
  docker compose up -d --wait
  docker compose exec -T postgres pg_isready -U marketplace -d marketplace
  test "$(docker compose exec -T valkey valkey-cli ping | tr -d '\r')" = "PONG"
  docker compose exec -T nats wget -q -O - http://127.0.0.1:8222/healthz >/dev/null
  echo "    PostgreSQL, Valkey, and NATS are healthy"
fi

echo "==> Verification completed"
