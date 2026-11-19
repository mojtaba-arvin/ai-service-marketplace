#!/usr/bin/env bash
set -Eeuo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

command -v gh >/dev/null || { echo "GitHub CLI (gh) is required" >&2; exit 1; }
gh auth status >/dev/null

python3 - <<'PY' | while IFS=$'\t' read -r name color description; do
import json
from pathlib import Path
for label in json.loads(Path(".github/labels.json").read_text(encoding="utf-8")):
    print(f"{label['name']}\t{label['color']}\t{label['description']}")
PY
  gh label create "$name" --color "$color" --description "$description" --force
done
