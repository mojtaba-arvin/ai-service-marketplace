# AI evaluation and prompt governance

This package provides deterministic, offline evaluation gates. It does not invoke an LLM; CI uses fixed predictions to verify regression thresholds and prompt/dataset contracts.

## Intake golden set

The versioned dataset at `golden/v2/intake.json` (dataset version `2.0.0`) defines category expectations, required details, critical cases, adversarial risk tags and minimum aggregate thresholds. Missing predictions count as failures. A critical case fails the run on either a category mismatch or any missing required detail, even if aggregate metrics pass.

The dataset is pinned by `prompts/intake/v1/manifest.json` (prompt `marketplace.intake.classifier@1.0.0`). The manifest also pins the output schema and deterministic decoding policy.

## Complete M14 product scope

`golden/product-scope/v1/cases.json` adds deterministic adversarial coverage for all seven product capabilities. It evaluates schema validity, critical-case correctness, required-detail recall and prohibited consequential actions. Every case is critical in the initial baseline; prohibited actions fail independently of aggregate scores. The matching `prompts/product-scope/v1/manifest.json` pins every capability's prompt ID/version and dataset version. A prompt change without a matching reviewed dataset update fails the contract test.

The cross-product evaluator emits a stable machine-readable JSON report and performs no model calls. Service-level tests for Intake, Pro Copilot and Dispute Triage verify redaction failure prevents model invocation and provenance fingerprints the redacted request input.

## Prompt lifecycle

Treat released prompt versions and golden datasets as immutable; create a new version and review its diff for every prompt or output-contract change. Update the prompt manifest and matching dataset in the same reviewed change. Promotion requires the offline evaluation gate to pass. A passing evaluator unit test is not evidence that a live model meets thresholds: provider-specific model evaluation is separate opt-in work and is not authorized by this issue.

## Run

From `apps/api`:

```sh
go test ./internal/aieval ./internal/aiintake ./internal/procopilot ./internal/disputetriage
```

Tests are offline and deterministic. No live API, model download or paid inference is required.
