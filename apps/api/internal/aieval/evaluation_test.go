package aieval

import (
	"errors"
	"os"
	"testing"
	"encoding/json"
	"bytes"
)

func loadGolden(t *testing.T) Dataset {
	t.Helper()
	raw,err:=os.ReadFile("golden/v1/intake.json");if err!=nil{t.Fatal(err)}
	var d Dataset
	if err:=json.Unmarshal(raw,&d);err!=nil{t.Fatal(err)}
	return d
}

func TestGoldenDatasetPassesDeterministicEvaluation(t *testing.T) {
	d:=loadGolden(t)
	predictions:=map[string]Prediction{
		"om-ac-not-cooling":{CategoryID:"ac-repair",RequiredDetails:[]string{"Unit model","Last service date"}},
		"om-water-leak":{CategoryID:"plumbing",RequiredDetails:[]string{"Leak location","When it started"}},
		"de-cleaning":{CategoryID:"home-cleaning",RequiredDetails:[]string{"Property size"}},
	}
	report,err:=Evaluate(d,predictions);if err!=nil{t.Fatal(err)}
	if !report.Passed||report.CategoryAccuracy!=1||report.DetailRecall!=1{t.Fatalf("unexpected report: %+v",report)}
}

func TestCriticalCaseRegressionFailsEvenWhenAggregateThresholdPasses(t *testing.T) {
	d:=loadGolden(t)
	predictions:=map[string]Prediction{
		"om-ac-not-cooling":{CategoryID:"cleaning",RequiredDetails:[]string{"Unit model","Last service date"}},
		"om-water-leak":{CategoryID:"plumbing",RequiredDetails:[]string{"Leak location","When it started"}},
		"de-cleaning":{CategoryID:"home-cleaning",RequiredDetails:[]string{"Property size"}},
	}
	report,err:=Evaluate(d,predictions)
	if !errors.Is(err,ErrRegression){t.Fatalf("expected regression, got %v",err)}
	if len(report.CriticalFailures)!=1||report.CriticalFailures[0]!="om-ac-not-cooling"{t.Fatalf("critical failure not reported: %+v",report)}
}

func TestMissingPredictionAndDetailRegressionFail(t *testing.T) {
	d:=loadGolden(t)
	predictions:=map[string]Prediction{
		"om-ac-not-cooling":{CategoryID:"ac-repair",RequiredDetails:[]string{"Unit model"}},
		"om-water-leak":{CategoryID:"plumbing",RequiredDetails:[]string{"Leak location","When it started"}},
	}
	report,err:=Evaluate(d,predictions)
	if !errors.Is(err,ErrRegression){t.Fatalf("expected regression, got %v",err)}
	if report.DetailRecall>=d.MinDetailRecall{t.Fatalf("expected detail recall regression: %+v",report)}
}

func TestInvalidDatasetRejected(t *testing.T) {
	d:=loadGolden(t);d.MinCategoryAccuracy=1.1
	if _,err:=Evaluate(d,map[string]Prediction{});!errors.Is(err,ErrInvalidDataset){t.Fatalf("got %v",err)}
	d=loadGolden(t);d.Cases=append(d.Cases,d.Cases[0])
	if _,err:=Evaluate(d,map[string]Prediction{});!errors.Is(err,ErrInvalidDataset){t.Fatalf("got %v",err)}
}


type promptManifest struct {
	PromptID string `json:"prompt_id"`
	Version string `json:"version"`
	OutputSchema string `json:"output_schema"`
	Dataset string `json:"dataset"`
}

func TestPromptManifestPinsTheGoldenDatasetAndOutputSchema(t *testing.T) {
	dataset := loadGolden(t)
	raw, err := os.ReadFile("prompts/intake/v1/manifest.json")
	if err != nil { t.Fatal(err) }
	var manifest promptManifest
	if err := json.Unmarshal(raw, &manifest); err != nil { t.Fatal(err) }
	if manifest.PromptID != dataset.PromptID || manifest.Version != dataset.PromptVersion {
		t.Fatalf("prompt/dataset version drift: manifest=%s@%s dataset=%s@%s",
			manifest.PromptID, manifest.Version, dataset.PromptID, dataset.PromptVersion)
	}
	if manifest.Dataset != "apps/api/internal/aieval/golden/v1/intake.json" ||
		manifest.OutputSchema != "apps/api/internal/aiintake/suggestion.schema.json" {
		t.Fatalf("manifest points to unexpected contract files: %+v", manifest)
	}
}

func TestEvaluationReportIsDeterministicAndMachineReadable(t *testing.T) {
	dataset := loadGolden(t)
	predictions := map[string]Prediction{
		"om-ac-not-cooling": {CategoryID:"ac-repair", RequiredDetails:[]string{"Unit model","Last service date"}},
		"om-water-leak": {CategoryID:"plumbing", RequiredDetails:[]string{"Leak location","When it started"}},
		"de-cleaning": {CategoryID:"home-cleaning", RequiredDetails:[]string{"Property size"}},
	}
	first, err := Evaluate(dataset, predictions)
	if err != nil { t.Fatal(err) }
	second, err := Evaluate(dataset, predictions)
	if err != nil { t.Fatal(err) }
	firstJSON, err := json.Marshal(first)
	if err != nil { t.Fatal(err) }
	secondJSON, err := json.Marshal(second)
	if err != nil { t.Fatal(err) }
	if !bytes.Equal(firstJSON, secondJSON) {
		t.Fatalf("evaluation report is not deterministic: %s != %s", firstJSON, secondJSON)
	}
	var decoded map[string]any
	if err := json.Unmarshal(firstJSON, &decoded); err != nil { t.Fatalf("report is not valid JSON: %v", err) }
	if decoded["passed"] != true || decoded["dataset_version"] != dataset.Version {
		t.Fatalf("machine report omitted gate result or dataset identity: %s", firstJSON)
	}
}
