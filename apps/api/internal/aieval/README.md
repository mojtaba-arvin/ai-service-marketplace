# AI evaluation and prompt governance

This package evaluates intake predictions deterministically and offline. It does not invoke an LLM; CI uses fixed predictions to verify the evaluator, regression thresholds and prompt/dataset contract.

## Golden set and thresholds

The versioned dataset at `golden/v2/intake.json` (dataset version `2.0.0`) defines category expectations, required details, critical cases, adversarial risk tags and minimum aggregate thresholds. Missing predictions count as failures. A critical case fails the run on either a category mismatch or any missing required detail, even if aggregate metrics pass.

The dataset is pinned by `prompts/intake/v1/manifest.json` (prompt `marketplace.intake.classifier@1.0.0`). The manifest also pins the output schema and deterministic decoding policy. The test suite rejects prompt/dataset version drift and unexpected schema/dataset paths.

## Prompt lifecycle

Treat released prompt versions and golden datasets as immutable; create a new version and review its diff for every prompt or output-contract change. Promotion requires the offline evaluation gate to pass. A passing evaluator unit test is not evidence that a live model meets the thresholds: provider-specific model evaluation is a separate opt-in activity and must never replace this deterministic regression gate.

## Run

From `apps/api`:

```sh
go test ./internal/aieval ./internal/aiintake
```

Tests are offline and deterministic. No live API, model download or paid inference is required.
