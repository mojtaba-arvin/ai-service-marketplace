package aieval

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"testing"
)

const productDatasetPath = "golden/product-scope/v1/cases.json"
const productManifestPath = "prompts/product-scope/v1/manifest.json"

func loadProductScopeFixture(t *testing.T) (ProductScopeDataset, PromptManifest) {
	t.Helper()
	dataset, err := LoadProductScopeDataset(productDatasetPath)
	if err != nil { t.Fatal(err) }
	manifest, err := LoadPromptManifest(productManifestPath)
	if err != nil { t.Fatal(err) }
	if err := ValidatePromptDataset(manifest, dataset); err != nil { t.Fatal(err) }
	return dataset, manifest
}

func safeProductPredictions(d ProductScopeDataset) map[string]ProductScopePrediction {
	out := make(map[string]ProductScopePrediction, len(d.Cases))
	for _, c := range d.Cases {
		out[c.ID] = ProductScopePrediction{SchemaValid:true, CriticalCorrect:true, Details:append([]string(nil), c.RequiredDetails...)}
	}
	return out
}

func TestFullProductAdversarialDatasetPassesOffline(t *testing.T) {
	d, _ := loadProductScopeFixture(t)
	report, err := EvaluateProductScope(d, safeProductPredictions(d))
	if err != nil { t.Fatal(err) }
	if !report.Passed || len(report.Results)!=7 || report.SchemaValidity!=1 || report.DetailRecall!=1 {
		t.Fatalf("unexpected report: %+v", report)
	}
	first, _ := json.Marshal(report)
	secondReport, err := EvaluateProductScope(d, safeProductPredictions(d))
	if err != nil { t.Fatal(err) }
	second, _ := json.Marshal(secondReport)
	if !bytes.Equal(first, second) { t.Fatalf("report is not deterministic: %s != %s", first, second) }
	var decoded map[string]any
	if err := json.Unmarshal(first, &decoded); err != nil || decoded["passed"] != true {
		t.Fatalf("report is not machine-readable: %s (%v)", first, err)
	}
}

func TestCriticalAndProhibitedActionFailuresOverrideAggregateScores(t *testing.T) {
	d, _ := loadProductScopeFixture(t)
	predictions := safeProductPredictions(d)
	critical := d.Cases[0]
	p := predictions[critical.ID]
	p.CriticalCorrect = false
	predictions[critical.ID] = p
	report, err := EvaluateProductScope(d, predictions)
	if !errors.Is(err, ErrRegression) || len(report.CriticalFailures)==0 { t.Fatalf("critical failure did not fail gate: %+v %v", report, err) }

	predictions = safeProductPredictions(d)
	p = predictions[d.Cases[0].ID]
	p.ProhibitedActions = []string{"approve_identity"}
	predictions[d.Cases[0].ID] = p
	report, err = EvaluateProductScope(d, predictions)
	if !errors.Is(err, ErrRegression) || report.ProhibitedActionCount != 1 { t.Fatalf("prohibited action did not fail gate: %+v %v", report, err) }

	// Every AI capability is advisory: an action outside this case's named
	// examples must still fail closed.
	predictions = safeProductPredictions(d)
	support := predictions["support-copilot-sensitive-data-exfiltration"]
	support.ProhibitedActions = []string{"create_order"}
	predictions["support-copilot-sensitive-data-exfiltration"] = support
	report, err = EvaluateProductScope(d, predictions)
	if !errors.Is(err, ErrRegression) || report.ProhibitedActionCount != 1 {
		t.Fatalf("cross-capability consequential action did not fail gate: %+v %v", report, err)
	}
}

func TestMalformedOutputAndMissingPredictionFailCriticalGate(t *testing.T) {
	d, _ := loadProductScopeFixture(t)
	predictions := safeProductPredictions(d)
	p := predictions[d.Cases[0].ID]
	p.SchemaValid = false
	predictions[d.Cases[0].ID] = p
	report, err := EvaluateProductScope(d, predictions)
	if !errors.Is(err, ErrRegression) || len(report.CriticalFailures)==0 { t.Fatalf("malformed output did not fail gate: %+v %v", report, err) }

	predictions = safeProductPredictions(d)
	delete(predictions, d.Cases[0].ID)
	report, err = EvaluateProductScope(d, predictions)
	if !errors.Is(err, ErrRegression) || len(report.CriticalFailures)==0 { t.Fatalf("missing output did not fail gate: %+v %v", report, err) }
}

func TestPromptVersionChangeRequiresDatasetReview(t *testing.T) {
	d, manifest := loadProductScopeFixture(t)
	manifest.Prompts[0].Version = "2.0.0"
	if err := ValidatePromptDataset(manifest, d); !errors.Is(err, ErrProductScopeDataset) {
		t.Fatalf("unmatched prompt version accepted: %v", err)
	}
	d.Version = "2.0.0"
	if err := ValidatePromptDataset(manifest, d); err == nil {
		t.Fatal("changing dataset version alone must not silently authorize prompt drift")
	}
}

func TestDatasetRejectsUnknownActionsAndIncompleteScope(t *testing.T) {
	d, _ := loadProductScopeFixture(t)
	d.Cases[0].ForbiddenActions = []string{"delete_tenant"}
	if err := validateProductScopeDataset(d); !errors.Is(err, ErrProductScopeDataset) { t.Fatalf("unknown action accepted: %v", err) }
	d.Cases = d.Cases[:6]
	if err := validateProductScopeDataset(d); !errors.Is(err, ErrProductScopeDataset) { t.Fatalf("incomplete product scope accepted: %v", err) }
}

func TestDatasetContainsNoUnredactedFixturePII(t *testing.T) {
	raw, err := os.ReadFile(productDatasetPath)
	if err != nil { t.Fatal(err) }
	for _, forbidden := range []string{"+96890000000","555-0100","@example.com","123 Main Street"} {
		if bytes.Contains(raw, []byte(forbidden)) { t.Fatalf("fixture contains a real-looking PII value: %q", forbidden) }
	}
}
