#!/usr/bin/env python3
"""Validate feature backlog shape, IDs, dependencies, cycles, and maintainer acceptance status rules."""
import json
import re
import sys
from pathlib import Path

path = Path("feature_list.json")
try:
    items = json.loads(path.read_text(encoding="utf-8"))
except (OSError, json.JSONDecodeError) as exc:
    print(f"feature backlog: {exc}", file=sys.stderr)
    raise SystemExit(1)
if not isinstance(items, list):
    raise SystemExit("feature backlog must be a JSON array")
required = {"id","module","title","description","acceptance","depends_on","size","nature","passes","verified_by","notes"}
allowed_nature = {"CRUD","WF","RT","FIN","ML-AI","INT","SEC","DOCS","DX"}
by_id = {}
errors = []
for i, item in enumerate(items):
    where = f"feature[{i}]"
    if not isinstance(item, dict) or set(item) != required:
        errors.append(f"{where}: expected exactly {sorted(required)}")
        continue
    fid = item["id"]
    if not isinstance(fid,str) or not re.fullmatch(r"[A-Z0-9][A-Z0-9-]+",fid):
        errors.append(f"{where}: invalid id")
    elif fid in by_id:
        errors.append(f"{where}: duplicate id {fid}")
    else:
        by_id[fid] = item
    for key in ("module","title","description","notes"):
        if not isinstance(item[key],str) or not item[key].strip():
            errors.append(f"{where}: {key} must be non-empty")
    if not isinstance(item["acceptance"],list) or not item["acceptance"] or not all(isinstance(x,str) and x.strip() for x in item["acceptance"]):
        errors.append(f"{where}: acceptance must contain non-empty strings")
    if not isinstance(item["depends_on"],list) or len(item["depends_on"]) != len(set(item["depends_on"])):
        errors.append(f"{where}: depends_on must be a unique list")
    if item["size"] not in {"S","M"}:
        errors.append(f"{where}: backlog items must be S/M; split larger work")
    if not isinstance(item["nature"],list) or not item["nature"] or not set(item["nature"]) <= allowed_nature:
        errors.append(f"{where}: invalid nature tags")
    if not isinstance(item["passes"],bool):
        errors.append(f"{where}: passes must be boolean")
    if item["passes"] and not item["verified_by"]:
        errors.append(f"{where}: passing feature requires maintainer evidence identity")
    if item["verified_by"] and not item["passes"]:
        errors.append(f"{where}: verified_by requires passes=true")
for fid,item in by_id.items():
    for dep in item["depends_on"]:
        if dep not in by_id:
            errors.append(f"{fid}: unknown dependency {dep}")
state = {}
def visit(fid, trail):
    if state.get(fid) == 1:
        errors.append("dependency cycle: " + " -> ".join(trail + [fid]))
        return
    if state.get(fid) == 2:
        return
    state[fid] = 1
    for dep in by_id[fid]["depends_on"]:
        if dep in by_id:
            visit(dep, trail + [fid])
    state[fid] = 2
for fid in by_id:
    visit(fid, [])
if errors:
    print("\n".join(errors), file=sys.stderr)
    raise SystemExit(1)
print(f"Validated {len(items)} features; {len(by_id)} unique IDs; dependency graph is acyclic.")
