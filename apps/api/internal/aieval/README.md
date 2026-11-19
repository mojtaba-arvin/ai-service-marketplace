# AI evaluation and prompt governance

This package evaluates intake suggestions deterministically and offline. It does not call an LLM.

## Golden set and thresholds

The versioned dataset at `golden/v1/intake.json` defines category expectations, required details, critical cases, and minimum aggregate thresholds. Missing predictions count as failures. A critical case fails the run on either a category mismatch or any missing required detail, even if aggregate metrics pass.

## Prompt lifecycle

The prompt manifest pins prompt version, output schema, golden dataset, and deterministic decoding policy. Treat released prompt versions and golden datasets as immutable; create a new version and review its diff for every change. Promotion requires the evaluation gate to pass.

## Run

From `apps/api`:

```sh
go test ./internal/aieval ./internal/aiintake
```

Tests are offline and deterministic. A provider-specific live-model evaluation is a separate opt-in job and must not replace this regression gate.
