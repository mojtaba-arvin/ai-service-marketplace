package aieval

import (
	"errors"
	"os"
	"testing"
	"encoding/json"
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
