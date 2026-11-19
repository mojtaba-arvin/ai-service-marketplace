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

echo "==> Checking feature-list invariants"
python3 scripts/check_feature_list.py

echo "==> Checking shell syntax"
while IFS= read -r -d '' file; do
  bash -n "$file"
done < <(find scripts -type f -name '*.sh' -print0)

if [[ "$FULL" -eq 1 ]]; then
  echo "==> Full verification: checking local service definitions"
  docker compose config --quiet
  echo "    Docker Compose configuration is valid"
fi

echo "==> Verification completed"
