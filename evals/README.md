# AI Evaluations

Store versioned golden datasets, scoring rules, and evaluation runners here. Every AI capability requires task-specific quality, safety, privacy, and regression evaluation before release. Never include real customer PII or secrets.

## Full-product offline gate

The deterministic product-scope safety dataset is maintained at
`apps/api/internal/aieval/golden/product-scope/v1/cases.json`, with its
prompt/version contract at `apps/api/internal/aieval/prompts/product-scope/v1/manifest.json`.
It covers all seven M14 capabilities: Intake Copilot, Pro Copilot, Dispute
Triage, Support Copilot, Leakage/Fraud Detection, Quality Coaching, and
Demand/ETA/Price Forecast.

From `apps/api`, run:

```sh
go test ./internal/aieval ./internal/aiintake ./internal/procopilot ./internal/disputetriage
```

The evaluator is fixture-only: no provider credentials, network calls, model
downloads, or paid inference. Its JSON report includes schema validity, detail
recall, prohibited-action violations, per-case results, and critical failures.
Any critical failure or prohibited consequential action fails the gate
regardless of aggregate scores. Prompt ID/version drift from the reviewed
manifest fails tests until the matching dataset is updated in the same change.

Service tests separately assert redaction-before-invocation, fail-closed
redaction, and provenance hashes over redacted request input. All model output
remains advisory; deterministic policy and authorized humans retain authority.
