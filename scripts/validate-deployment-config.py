#!/usr/bin/env python3
"""Validate production Compose inputs without sourcing or printing secret values."""
import json
import re
import sys
from pathlib import Path
from urllib.parse import unquote, urlsplit

REQUIRED = (
    "SITE_ADDRESS", "POSTGRES_DB", "POSTGRES_USER", "POSTGRES_PASSWORD",
    "DATABASE_URL", "OTP_DELIVERY_URL", "OTP_DELIVERY_TOKEN", "OTP_PEPPER",
    "SESSION_PEPPER", "DEFAULT_TENANT_ID", "DEFAULT_MARKET_CODE",
    "BOOTSTRAP_OPERATOR_PHONE", "EVIDENCE_ACTIVE_KEY_ID",
    "EVIDENCE_ENCRYPTION_KEYS", "EVIDENCE_ALLOWED_MEDIA_TYPES",
    "EVIDENCE_CLAMD_ADDRESS",
)
PLACEHOLDERS = ("replace-with", "example.com", "example.test", "changeme", "ci-only", "<")

def parse_env(path):
    values = {}
    for number, raw in enumerate(Path(path).read_text(encoding="utf-8").splitlines(), 1):
        line = raw.strip()
        if not line or line.startswith("#"):
            continue
        if "=" not in line:
            raise ValueError(f"line {number}: expected KEY=value")
        key, value = line.split("=", 1)
        key, value = key.strip(), value.strip()
        if not re.fullmatch(r"[A-Z][A-Z0-9_]*", key):
            raise ValueError(f"line {number}: invalid environment variable name")
        if len(value) >= 2 and value[0] == value[-1] and value[0] in ("'", '"'):
            value = value[1:-1]
        values[key] = value
    return values

def validate(values):
    errors = []
    for key in REQUIRED:
        value = values.get(key, "").strip()
        if not value:
            errors.append(f"{key} is required")
        elif any(token in value.lower() for token in PLACEHOLDERS):
            errors.append(f"{key} still contains a placeholder value")

    site = values.get("SITE_ADDRESS", "").strip()
    if site and ("://" in site or "/" in site or not re.fullmatch(r"(?=.{1,253}$)(?:[A-Za-z0-9](?:[A-Za-z0-9-]{0,61}[A-Za-z0-9])?\.)+[A-Za-z]{2,63}", site)):
        errors.append("SITE_ADDRESS must be a DNS hostname without a scheme or path")
    if site.lower().endswith((".test", ".invalid", ".localhost")) or ".example." in site.lower():
        errors.append("SITE_ADDRESS must not use a reserved example or test domain")

    otp = values.get("OTP_DELIVERY_URL", "").strip()
    if otp:
        parsed = urlsplit(otp)
        if parsed.scheme != "https" or not parsed.hostname or parsed.username or parsed.password:
            errors.append("OTP_DELIVERY_URL must be an HTTPS URL without embedded credentials")
        elif parsed.hostname.endswith((".example", ".test", ".invalid", ".localhost")):
            errors.append("OTP_DELIVERY_URL must identify a real provider endpoint")

    for key in ("POSTGRES_PASSWORD", "OTP_DELIVERY_TOKEN"):
        value = values.get(key, "")
        if value and len(value) < 24:
            errors.append(f"{key} must contain at least 24 characters")
    for key in ("OTP_PEPPER", "SESSION_PEPPER"):
        value = values.get(key, "")
        if value and len(value.encode("utf-8")) < 32:
            errors.append(f"{key} must contain at least 32 bytes")
    if values.get("OTP_PEPPER") and values.get("OTP_PEPPER") == values.get("SESSION_PEPPER"):
        errors.append("OTP_PEPPER and SESSION_PEPPER must be different")

    market = values.get("DEFAULT_MARKET_CODE", "")
    if market and not re.fullmatch(r"[A-Za-z]{2}", market):
        errors.append("DEFAULT_MARKET_CODE must be a two-letter ISO country code")
    phone = values.get("BOOTSTRAP_OPERATOR_PHONE", "")
    if phone and not re.fullmatch(r"\+[1-9][0-9]{7,14}", phone):
        errors.append("BOOTSTRAP_OPERATOR_PHONE must be an E.164 number")

    database_url = values.get("DATABASE_URL", "")
    if database_url:
        parsed = urlsplit(database_url)
        if parsed.scheme not in ("postgres", "postgresql") or parsed.hostname != "postgres" or not parsed.path.lstrip("/"):
            errors.append("DATABASE_URL must target the private Compose postgres service and a database")
        else:
            if unquote(parsed.username or "") != values.get("POSTGRES_USER", ""):
                errors.append("DATABASE_URL username must match POSTGRES_USER")
            if unquote(parsed.password or "") != values.get("POSTGRES_PASSWORD", ""):
                errors.append("DATABASE_URL password must match POSTGRES_PASSWORD")
            if parsed.path.lstrip("/") != values.get("POSTGRES_DB", ""):
                errors.append("DATABASE_URL database must match POSTGRES_DB")

    key_id, encoded = values.get("EVIDENCE_ACTIVE_KEY_ID", ""), values.get("EVIDENCE_ENCRYPTION_KEYS", "")
    if encoded:
        try:
            keys = json.loads(encoded)
            if not isinstance(keys, dict) or not keys:
                raise ValueError()
            for item_id, key in keys.items():
                if not isinstance(item_id, str) or not 1 <= len(item_id) <= 64 or not isinstance(key, str) or not re.fullmatch(r"[0-9a-fA-F]{64}", key):
                    raise ValueError()
            if key_id not in keys:
                errors.append("EVIDENCE_ACTIVE_KEY_ID must exist in EVIDENCE_ENCRYPTION_KEYS")
        except (ValueError, TypeError):
            errors.append("EVIDENCE_ENCRYPTION_KEYS must be a JSON object of key IDs to 32-byte hex keys")

    if not [x.strip() for x in values.get("EVIDENCE_ALLOWED_MEDIA_TYPES", "").split(",") if x.strip()]:
        errors.append("EVIDENCE_ALLOWED_MEDIA_TYPES must list at least one media type")
    clamd = values.get("EVIDENCE_CLAMD_ADDRESS", "")
    if clamd and (not re.fullmatch(r"[A-Za-z0-9.-]+:[0-9]{1,5}", clamd) or clamd.startswith(("localhost:", "127.0.0.1:", "0.0.0.0:"))):
        errors.append("EVIDENCE_CLAMD_ADDRESS must identify a private clamd host:port")
    if clamd and clamd.rsplit(":", 1)[-1].isdigit() and not 1 <= int(clamd.rsplit(":", 1)[-1]) <= 65535:
        errors.append("EVIDENCE_CLAMD_ADDRESS port must be between 1 and 65535")
    return errors

def self_test():
    sample = {
        "SITE_ADDRESS": "market.demo.org", "POSTGRES_DB": "marketplace",
        "POSTGRES_USER": "marketplace", "POSTGRES_PASSWORD": "a-strong-password-with-sufficient-length",
        "DATABASE_URL": "postgres://marketplace:a-strong-password-with-sufficient-length@postgres:5432/marketplace?sslmode=disable",
        "OTP_DELIVERY_URL": "https://sms.vendor.org/v1/messages",
        "OTP_DELIVERY_TOKEN": "provider-token-with-enough-entropy",
        "OTP_PEPPER": "a" * 64, "SESSION_PEPPER": "b" * 64,
        "DEFAULT_TENANT_ID": "market-om", "DEFAULT_MARKET_CODE": "OM",
        "BOOTSTRAP_OPERATOR_PHONE": "+96890000000", "EVIDENCE_ACTIVE_KEY_ID": "2026-11",
        "EVIDENCE_ENCRYPTION_KEYS": json.dumps({"2026-11": "c" * 64}),
        "EVIDENCE_ALLOWED_MEDIA_TYPES": "application/pdf,image/jpeg",
        "EVIDENCE_CLAMD_ADDRESS": "clamav.internal:3310",
    }
    assert not validate(sample), validate(sample)
    assert any("OTP_PEPPER" in e for e in validate({**sample, "OTP_PEPPER": "short"}))
    assert any("DATABASE_URL" in e for e in validate({**sample, "DATABASE_URL": "postgres://user:pass@public.example.org/db"}))
    assert any("EVIDENCE_ACTIVE_KEY_ID" in e for e in validate({**sample, "EVIDENCE_ACTIVE_KEY_ID": "missing"}))
    assert any("placeholder" in e for e in validate({**sample, "OTP_DELIVERY_TOKEN": "replace-with-token"}))
    assert any("SITE_ADDRESS" in e for e in validate({**sample, "SITE_ADDRESS": "market.example.org"}))
    assert any("POSTGRES_PASSWORD" in e for e in validate({**sample, "POSTGRES_PASSWORD": "short"}))
    print("Production deployment configuration validator self-tests passed")

def main():
    if len(sys.argv) == 2 and sys.argv[1] == "--self-test":
        self_test()
        return 0
    if len(sys.argv) != 2:
        print("Usage: validate-deployment-config.py .env.production | --self-test", file=sys.stderr)
        return 2
    try:
        values = parse_env(sys.argv[1])
    except (OSError, ValueError) as exc:
        print(f"Configuration file error: {exc}", file=sys.stderr)
        return 2
    errors = validate(values)
    if errors:
        print("Production deployment configuration is invalid:", file=sys.stderr)
        for error in errors:
            print(f" - {error}", file=sys.stderr)
        return 1
    print("Production deployment configuration is valid (secret values were not displayed)")
    return 0

if __name__ == "__main__":
    raise SystemExit(main())
