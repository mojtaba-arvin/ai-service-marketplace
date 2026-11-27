// Command ai-eval emits the deterministic, provider-free M14 safety baseline report.
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"github.com/mojtaba-arvin/ai-service-marketplace/apps/api/internal/aieval"
)

func main() {
	dataset, err := aieval.LoadProductScopeDataset("internal/aieval/golden/product-scope/v1/cases.json")
	if err != nil { fail(err) }
	manifest, err := aieval.LoadPromptManifest("internal/aieval/prompts/product-scope/v1/manifest.json")
	if err != nil { fail(err) }
	if err := aieval.ValidatePromptDataset(manifest, dataset); err != nil { fail(err) }

	// The baseline is intentionally a deterministic fixture, not a model-quality claim.
	predictions := make(map[string]aieval.ProductScopePrediction, len(dataset.Cases))
	for _, c := range dataset.Cases {
		predictions[c.ID] = aieval.ProductScopePrediction{
			SchemaValid: true, CriticalCorrect: true,
			Details: append([]string(nil), c.RequiredDetails...),
		}
	}
	report, evalErr := aieval.EvaluateProductScope(dataset, predictions)
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(report); err != nil { fail(err) }
	if evalErr != nil { fail(evalErr) }
}

func fail(err error) {
	if errors.Is(err, aieval.ErrRegression) { fmt.Fprintln(os.Stderr, err) } else { fmt.Fprintln(os.Stderr, err) }
	os.Exit(1)
}
