# AI intake suggestion boundary

This package turns an untrusted model response into a constrained intake suggestion. It is deliberately advisory and has no persistence or command-execution capability.

## Trust boundaries

- The catalog interface supplies the only eligible category IDs; model output outside that allowlist is rejected.
- Model JSON is decoded strictly: unknown fields, trailing JSON, malformed values, invalid confidence, and excessive lengths are rejected.
- The output schema contains no payable amount, payment, identity-verification, safety-approval, or reputation-decision fields. Unknown fields are forbidden.
- Provenance is attached by the service, not trusted from the model: model name/version, SHA-256 of normalized input, and generation timestamp.
- Suggestions are returned to the caller only. A separate deterministic workflow and authorized human/operator action must decide any consequential outcome.

## Provider integration

Implement the Model interface behind the external-adapter execution controls. Keep credentials and provider SDKs outside this package. Implement Catalog with the market-scoped catalog application API; never pass unrestricted database access to the model.

## Verification

Run go test ./internal/aiintake from apps/api. Tests use local fakes and require no model credentials or network access.
