package procopilot
import("context";"encoding/json";"errors";"strings";"testing";"time")
type fakeKnowledge struct{evidence []Evidence;err error}
func(f fakeKnowledge)Search(_ context.Context,_,_,_,_ string)([]Evidence,error){return f.evidence,f.err}
type fakeRedactor struct{err error}
func(f fakeRedactor)Redact(_ context.Context,v string)(string,error){if f.err!=nil{return "",f.err};return strings.ReplaceAll(v,"555-0100","[REDACTED]"),nil}
type fakeModel struct{input ModelInput;output ModelOutput;err error}
func(f *fakeModel)Generate(_ context.Context,in ModelInput)(ModelOutput,error){f.input=in;return f.output,f.err}
func validOutput()[]byte{return []byte(`{"diagnosis_summary":"Possible blocked condensate drain; confirm before repair.","safety_warnings":["Isolate electrical power before opening the unit."],"steps":["Inspect the drain line for visible blockage.","Check the condensate tray for overflow."],"checklist":["Confirm power isolation","Inspect drain line","Photograph existing damage"],"quote_scope_draft":"Inspect and clear the condensate drain; replacement parts require customer approval.","confidence":0.82,"citations":[{"id":"manual-ac-01"}]}`)}
func TestSuggestRedactsAndRequiresReview(t *testing.T){
 model:=&fakeModel{output:ModelOutput{JSON:validOutput(),Name:"local-model",Version:"q4-v1"}}
 svc:=Service{Knowledge:fakeKnowledge{evidence:[]Evidence{{ID:"manual-ac-01",Source:"manufacturer-manual",Text:"Drain inspection for model 555-0100."}}},Model:model,Redactor:fakeRedactor{},Timeout:time.Second,Now:func()time.Time{return time.Date(2026,11,23,8,0,0,0,time.UTC)}}
 got,err:=svc.Suggest(context.Background(),Request{Market:"OM",Locale:"en",ServiceCode:"ac-repair",Description:"AC leaking; call 555-0100",EquipmentContext:"Split AC unit"});if err!=nil{t.Fatal(err)}
 if !got.RequiresProfessionalReview{t.Fatal("professional review must always be required")}
 if strings.Contains(model.input.Description,"555-0100")||strings.Contains(model.input.Evidence[0].Text,"555-0100"){t.Fatal("unredacted PII reached model")}
 if len(got.Citations)!=1||got.Citations[0].ID!="manual-ac-01"||got.Provenance.InputSHA256==""||got.Provenance.GeneratedAt.IsZero(){t.Fatalf("unexpected result: %#v",got)}
}
func TestSuggestRejectsInventedCitationsAndMoneyFields(t *testing.T){
 payload:=map[string]any{};if err:=json.Unmarshal(validOutput(),&payload);err!=nil{t.Fatal(err)}
 payload["citations"]=[]any{map[string]any{"id":"invented"}};encoded,_:=json.Marshal(payload)
 model:=&fakeModel{output:ModelOutput{JSON:encoded,Name:"model",Version:"1"}}
 svc:=Service{Knowledge:fakeKnowledge{evidence:[]Evidence{{ID:"allowed",Source:"manual",Text:"Safe source"}}},Model:model,Redactor:fakeRedactor{},Timeout:time.Second}
 req:=Request{Market:"OM",Locale:"en",ServiceCode:"repair",Description:"Issue"}
 if _,err:=svc.Suggest(context.Background(),req);!errors.Is(err,ErrInvalidSuggestion){t.Fatalf("invented citation accepted: %v",err)}
 model.output.JSON=[]byte(strings.TrimSuffix(string(validOutput()),"}")+", \"payable_amount_minor\": 1}")
 if _,err:=svc.Suggest(context.Background(),req);!errors.Is(err,ErrInvalidSuggestion){t.Fatalf("unknown money field accepted: %v",err)}
}
func TestSuggestFailsClosed(t *testing.T){
 req:=Request{Market:"OM",Locale:"en",ServiceCode:"repair",Description:"Issue"}
 svc:=Service{Knowledge:fakeKnowledge{evidence:[]Evidence{{ID:"e1",Source:"manual",Text:"Source"}}},Model:&fakeModel{output:ModelOutput{JSON:validOutput(),Name:"m",Version:"1"}},Redactor:fakeRedactor{err:errors.New("redaction unavailable")},Timeout:time.Second}
 if _,err:=svc.Suggest(context.Background(),req);err==nil{t.Fatal("redaction failure must fail closed")}
 svc.Redactor=fakeRedactor{};svc.Knowledge=fakeKnowledge{}
 if _,err:=svc.Suggest(context.Background(),req);!errors.Is(err,ErrInvalidRequest){t.Fatalf("missing evidence must fail closed: %v",err)}
}
