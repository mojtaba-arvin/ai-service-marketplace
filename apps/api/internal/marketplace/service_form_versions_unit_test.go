package marketplace

import(
 "encoding/json"
 "strings"
 "testing"
 "time"
)
func TestPublicServiceFormOmitsOperatorIdentity(t *testing.T){
 published:=time.Date(2026,11,23,10,0,0,0,time.UTC)
 form:=publicServiceForm(ServiceFormVersion{ID:"form-1",MenuID:"menu-1",ServiceCode:"ac-repair",MarketCode:"OM",Locale:"en",Version:2,Schema:json.RawMessage(`{"type":"object"}`),CreatedBy:"private-creator",PublishedBy:stringPointer("private-publisher"),PublishedAt:&published,Status:"published"})
 raw,err:=json.Marshal(form);if err!=nil{t.Fatal(err)}
 text:=string(raw)
 if strings.Contains(text,"private-creator")||strings.Contains(text,"private-publisher")||strings.Contains(text,"created_by")||strings.Contains(text,"published_by"){t.Fatalf("public form leaked operator metadata: %s",text)}
 if !strings.Contains(text,"ac-repair")||!strings.Contains(text,"published_at"){t.Fatalf("public form omitted required fields: %s",text)}
}
func stringPointer(v string)*string{return &v}
