package disputetriage
import("path/filepath";"testing")
func TestDisputeTriageGoldenEvaluation(t *testing.T){
 d,err:=LoadEvalDataset(filepath.Join("golden","v1","disputes.json"));if err!=nil{t.Fatal(err)}
 predictions:=map[string]Triage{
 "incomplete-work":{RequiresHumanAdjudication:true,ResolutionOptions:[]ResolutionOption{{Citations:[]Citation{{EvidenceID:"order-policy-01"}}}}},
 "damage-after-service":{RequiresHumanAdjudication:true,ResolutionOptions:[]ResolutionOption{{Citations:[]Citation{{EvidenceID:"damage-photo-02"},{EvidenceID:"service-record-01"}}}}}}
 report,err:=Evaluate(d,predictions,1);if err!=nil{t.Fatal(err)};if !report.Passed||report.EvidenceRecall!=1{t.Fatalf("unexpected report: %#v",report)}
 delete(predictions,"damage-after-service");if _,err=Evaluate(d,predictions,1);err==nil{t.Fatal("missing critical dispute case must fail")}
}
