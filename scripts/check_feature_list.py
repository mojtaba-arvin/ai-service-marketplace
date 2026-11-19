#!/usr/bin/env python3
"""Validate the feature backlog's stable schema and status invariants."""
import json
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
data = json.loads((ROOT / "feature_list.json").read_text(encoding="utf-8"))

if not isinstance(data, list):
    raise SystemExit("feature_list.json must contain a JSON array")

required = {"id", "module", "title", "description", "acceptance", "depends_on",
            "size", "nature", "passes", "verified_by", "notes"}
seen = set()
valid_sizes = {"S", "M", "L"}
for index, item in enumerate(data):
    missing = required - item.keys()
    if missing:
        raise SystemExit(f"feature[{index}] missing fields: {sorted(missing)}")
    if item["id"] in seen:
        raise SystemExit(f"duplicate feature id: {item['id']}")
    seen.add(item["id"])
    if item["size"] not in valid_sizes:
        raise SystemExit(f"{item['id']}: invalid size")
    if not isinstance(item["passes"], bool):
        raise SystemExit(f"{item['id']}: passes must be boolean")
    if item["passes"] and not item["verified_by"]:
        raise SystemExit(f"{item['id']}: passing feature requires verified_by")
    if not isinstance(item["acceptance"], list) or not item["acceptance"]:
        raise SystemExit(f"{item['id']}: acceptance must be a non-empty list")

unknown = {
    dependency
    for item in data
    for dependency in item["depends_on"]
    if dependency not in seen
}
if unknown:
    raise SystemExit(f"unknown dependencies: {sorted(unknown)}")

print(f"Validated {len(data)} feature records")
