package disputetriage
import("context";"errors";"strings";"testing";"time")
type fakeEvidence struct{items []Evidence;err error}
func(f fakeEvidence)Retrieve(_ context.Context,_,_,_,_ string)([]Evidence,error){return f.items,f.err}
type fakeRedactor struct{err error}
func(f fakeRedactor)Redact(_ context.Context,v string)(string,error){if f.err!=nil{return "",f.err};return strings.ReplaceAll(v,"+96890000000","[PHONE]"),nil}
type fakeModel struct{input ModelInput;output ModelOutput;err error}
func(f *fakeModel)Generate(_ context.Context,in ModelInput)(ModelOutput,error){f.input=in;return f.output,f.err}
func validOutput()[]byte{return []byte(`{"case_summary":"Customer reports incomplete repair; professional reports completion.","established_facts":["Order was marked completed by the professional."],"missing_information":["Customer confirmation is not recorded.","Completion photos are not attached."],"resolution_options":[{"description":"Request missing completion evidence before adjudication.","citations":[{"evidence_id":"order-policy-01"}]}],"confidence":0.76}`)}
func TestTriageRedactsAndRequiresHumanAdjudication(t *testing.T){
 model:=&fakeModel{output:ModelOutput{JSON:validOutput(),Name:"local",Version:"v1"}}
 svc:=Service{Evidence:fakeEvidence{items:[]Evidence{{ID:"order-policy-01",Source:"policy",Text:"Completion requires customer confirmation; contact +96890000000."}}},Redactor:fakeRedactor{},Model:model,Timeout:time.Second,Now:func()time.Time{return time.Date(2026,11,23,9,0,0,0,time.UTC)}}
 got,err:=svc.Triage(context.Background(),Request{Market:"OM",Locale:"en",DisputeType:"incomplete-work",Reason:"Customer called +96890000000 and reports incomplete work."})
 if err!=nil{t.Fatal(err)}
 if !got.RequiresHumanAdjudication{t.Fatal("human adjudication must always be required")}
 if strings.Contains(model.input.Reason,"+96890000000")||strings.Contains(model.input.Evidence[0].Text,"+96890000000"){t.Fatal("unredacted phone reached model")}
 if got.Provenance.InputSHA256==""||len(got.Provenance.EvidenceIDs)!=1||got.Provenance.GeneratedAt.IsZero(){t.Fatalf("missing provenance: %#v",got.Provenance)}
}
func TestTriageRejectsInventedEvidenceAndAuthorityFields(t *testing.T){
 model:=&fakeModel{output:ModelOutput{JSON:[]byte(strings.Replace(string(validOutput()),"order-policy-01","invented-source",1)),Name:"m",Version:"1"}}
 svc:=Service{Evidence:fakeEvidence{items:[]Evidence{{ID:"allowed",Source:"policy",Text:"Policy text"}}},Redactor:fakeRedactor{},Model:model,Timeout:time.Second}
 req:=Request{Market:"OM",Locale:"en",DisputeType:"refund",Reason:"Case"}
 if _,err:=svc.Triage(context.Background(),req);!errors.Is(err,ErrInvalidTriage){t.Fatalf("invented citation accepted: %v",err)}
 model.output.JSON=[]byte(strings.TrimSuffix(string(validOutput()),"}")+", \"decision\": \"refund\"}")
 if _,err:=svc.Triage(context.Background(),req);!errors.Is(err,ErrInvalidTriage){t.Fatalf("authority field accepted: %v",err)}
}
func TestTriageFailsClosedOnRedactionOrMissingEvidence(t *testing.T){
 req:=Request{Market:"OM",Locale:"en",DisputeType:"refund",Reason:"Case"}
 svc:=Service{Evidence:fakeEvidence{items:[]Evidence{{ID:"e1",Source:"policy",Text:"Text"}}},Redactor:fakeRedactor{err:errors.New("redaction unavailable")},Model:&fakeModel{output:ModelOutput{JSON:validOutput(),Name:"m",Version:"1"}},Timeout:time.Second}
 if _,err:=svc.Triage(context.Background(),req);err==nil{t.Fatal("redaction failure must fail closed")}
 svc.Redactor=fakeRedactor{};svc.Evidence=fakeEvidence{}
 if _,err:=svc.Triage(context.Background(),req);!errors.Is(err,ErrInvalidCase){t.Fatalf("empty evidence accepted: %v",err)}
}
