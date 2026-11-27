#!/usr/bin/env python3
"""Validate the repository's self-contained OpenAPI contract without network access."""
from __future__ import annotations

import json
import re
import sys
from pathlib import Path
from typing import Any

ROOT = Path(__file__).resolve().parents[1]
SPEC = ROOT / "packages" / "contracts" / "openapi.v1.json"
HTTP_METHODS = {"get", "post", "put", "patch", "delete", "options", "head", "trace"}
PATH_PARAMETER = re.compile(r"\{([^{}]+)\}")


def unique_object(pairs: list[tuple[str, Any]]) -> dict[str, Any]:
    result: dict[str, Any] = {}
    for key, value in pairs:
        if key in result:
            raise ValueError(f"duplicate JSON object key: {key}")
        result[key] = value
    return result


def pointer_target(document: Any, ref: str) -> Any:
    if not ref.startswith("#/"):
        raise ValueError(f"external or unsupported reference is not allowed: {ref}")
    current = document
    for encoded in ref[2:].split("/"):
        token = encoded.replace("~1", "/").replace("~0", "~")
        if not isinstance(current, dict) or token not in current:
            raise ValueError(f"unresolved reference: {ref}")
        current = current[token]
    return current


def walk(value: Any, path: str = "$"):
    if isinstance(value, dict):
        yield path, value
        for key, child in value.items():
            yield from walk(child, f"{path}/{key}")
    elif isinstance(value, list):
        for index, child in enumerate(value):
            yield from walk(child, f"{path}/{index}")


def fail(message: str) -> None:
    raise ValueError(message)


def main() -> int:
    try:
        document = json.loads(SPEC.read_text(encoding="utf-8"), object_pairs_hook=unique_object)
        if not isinstance(document, dict) or not str(document.get("openapi", "")).startswith("3.1."):
            fail("OpenAPI document must declare version 3.1.x")
        info = document.get("info")
        if not isinstance(info, dict) or not info.get("title") or not info.get("version"):
            fail("OpenAPI info.title and info.version are required")

        paths = document.get("paths")
        if not isinstance(paths, dict) or not paths:
            fail("OpenAPI paths must be a non-empty object")

        operation_ids: set[str] = set()
        operation_count = 0
        for path, path_item in paths.items():
            if not isinstance(path, str) or not path.startswith("/"):
                fail(f"invalid API path: {path!r}")
            if not isinstance(path_item, dict):
                fail(f"path item must be an object: {path}")
            required_path_names = set(PATH_PARAMETER.findall(path))
            path_parameters = path_item.get("parameters", [])
            if not isinstance(path_parameters, list):
                fail(f"path-level parameters must be an array: {path}")
            for method, operation in path_item.items():
                if method.lower() not in HTTP_METHODS:
                    continue
                operation_count += 1
                if not isinstance(operation, dict):
                    fail(f"operation must be an object: {method.upper()} {path}")
                operation_id = operation.get("operationId")
                if not isinstance(operation_id, str) or not operation_id.strip():
                    fail(f"operationId is required: {method.upper()} {path}")
                if operation_id in operation_ids:
                    fail(f"duplicate operationId: {operation_id}")
                operation_ids.add(operation_id)
                responses = operation.get("responses")
                if not isinstance(responses, dict) or not responses:
                    fail(f"responses are required: {method.upper()} {path}")

                operation_parameters = operation.get("parameters", [])
                if not isinstance(operation_parameters, list):
                    fail(f"operation parameters must be an array: {method.upper()} {path}")
                all_parameters = path_parameters + operation_parameters
                declared = {
                    parameter.get("name")
                    for parameter in all_parameters
                    if isinstance(parameter, dict)
                    and parameter.get("in") == "path"
                    and parameter.get("required") is True
                }
                missing = required_path_names - declared
                if missing:
                    fail(f"missing required path parameters for {method.upper()} {path}: {sorted(missing)}")

        if operation_count == 0:
            fail("OpenAPI document contains no HTTP operations")

        for location, value in walk(document):
            if "$ref" in value:
                pointer_target(document, value["$ref"])
            if "security" in value and isinstance(value["security"], list):
                for requirement in value["security"]:
                    if not isinstance(requirement, dict):
                        fail(f"invalid security requirement at {location}")
                    for scheme in requirement:
                        if scheme not in document.get("components", {}).get("securitySchemes", {}):
                            fail(f"undefined security scheme {scheme!r} at {location}")

        print(
            f"OpenAPI contract OK: {operation_count} operations, "
            f"{len(paths)} paths, {len(operation_ids)} unique operationIds"
        )
        return 0
    except (OSError, json.JSONDecodeError, ValueError, TypeError) as error:
        print(f"OpenAPI contract validation failed: {error}", file=sys.stderr)
        return 1


if __name__ == "__main__":
    raise SystemExit(main())
