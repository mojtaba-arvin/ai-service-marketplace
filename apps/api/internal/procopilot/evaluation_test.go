package procopilot
import("path/filepath";"testing")
func TestProfessionalCopilotGoldenEvaluation(t *testing.T){
 d,err:=LoadEvaluationDataset(filepath.Join("golden","v1","professional.json"));if err!=nil{t.Fatal(err)}
 predictions:=map[string]Suggestion{
  "om-ac-drain-leak":{Checklist:[]string{"Confirm power isolation","Inspect drain line"},Citations:[]EvidenceReference{{ID:"manual-ac-01"}},RequiresProfessionalReview:true},
  "om-water-heater-no-heat":{Checklist:[]string{"Verify power supply","Check thermostat setting"},Citations:[]EvidenceReference{{ID:"manual-water-heater-02"}},RequiresProfessionalReview:true},
 }
 report,err:=Evaluate(d,predictions,0.9,0.9);if err!=nil{t.Fatal(err)}
 if !report.Passed||report.ChecklistRecall!=1||report.CitationRecall!=1{t.Fatalf("unexpected report: %#v",report)}
 delete(predictions,"om-water-heater-no-heat");if _,err=Evaluate(d,predictions,0.9,0.9);err==nil{t.Fatal("missing critical prediction must fail")}
}
