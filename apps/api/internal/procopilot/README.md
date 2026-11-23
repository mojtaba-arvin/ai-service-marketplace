# Professional Copilot boundary

This package provides an advisory, evidence-grounded boundary for professional troubleshooting, execution checklists and quote-scope drafting. It does not execute work, set payable amounts, approve quotes, change orders or decide safety.

- User and retrieved evidence text are redacted before model invocation.
- Retrieval is scoped by market, locale and service code; the model may cite only retrieved evidence.
- Output is strict JSON with bounded fields. Unknown fields (including money/approval fields), trailing content and invented citations are rejected.
- Provenance includes model/version, input fingerprint, evidence IDs and generation time.
- Every suggestion sets requires_professional_review=true. A professional must verify diagnosis, safety steps and scope.
- Provider, retrieval or redaction failure is fail-closed. The package has no persistence or command execution capability.

Implement Model, Knowledge and Redactor behind provider controls. Never pass unrestricted database access or raw PII to a model. Credentials remain outside this package.

Run go test ./internal/procopilot from apps/api. Tests use local fakes and no live model credentials.
