package aiintake

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

type identityRedactor struct{}
func (identityRedactor) Redact(_ context.Context,text string)(string,error){return text,nil}

type fakeRedactor struct { text string; err error }
func (f fakeRedactor) Redact(_ context.Context,_ string)(string,error){return f.text,f.err}

type fakeCatalog struct { categories []Category; err error }
func (f fakeCatalog) EnabledCategories(context.Context,string,string)([]Category,error){return f.categories,f.err}

type fakeModel struct { output ModelOutput; err error; input ModelInput }
func (f *fakeModel) Generate(_ context.Context,input ModelInput)(ModelOutput,error){f.input=input;return f.output,f.err}

func serviceFor(raw string) (Service,*fakeModel) {
	model:=&fakeModel{output:ModelOutput{JSON:[]byte(raw),Name:"fake-intake",Version:"v1"}}
	return Service{
		Redactor:identityRedactor{},
		Catalog:fakeCatalog{categories:[]Category{{ID:"cleaning",Label:"Cleaning"},{ID:"ac-repair",Label:"AC repair"}}},
		Model:model,Timeout:time.Second,Now:func()time.Time{return time.Date(2026,11,19,12,0,0,0,time.UTC)},
	},model
}

func TestSuggestReturnsValidatedSuggestionAndServiceOwnedProvenance(t *testing.T) {
	s,model:=serviceFor(`{"category_id":"ac-repair","summary":"AC is not cooling","required_details":["Unit model","Last service date"],"confidence":0.91}`)
	got,err:=s.Suggest(context.Background(),Request{Market:"OM",Locale:"en-OM",Text:"  AC is not cooling  "})
	if err!=nil {t.Fatal(err)}
	if got.CategoryID!="ac-repair" || got.Confidence!=0.91 {t.Fatalf("unexpected suggestion: %+v",got)}
	if got.Provenance.Model!="fake-intake" || got.Provenance.ModelVersion!="v1" || got.Provenance.InputSHA256=="" {t.Fatalf("missing provenance: %+v",got.Provenance)}
	if !got.Provenance.GeneratedAt.Equal(time.Date(2026,11,19,12,0,0,0,time.UTC)) {t.Fatalf("unexpected timestamp: %v",got.Provenance.GeneratedAt)}
	if model.input.Market!="OM" || len(model.input.AllowedCategories)!=2 {t.Fatalf("catalog context not passed to model: %+v",model.input)}
}

func TestSuggestRejectsUnknownOrForbiddenFields(t *testing.T) {
	cases:=[]string{
		`{"category_id":"unknown","summary":"x","required_details":[],"confidence":0.5}`,
		`{"category_id":"cleaning","summary":"x","required_details":[],"confidence":0.5,"price_minor":100}`,
		`{"category_id":"cleaning","summary":"x","required_details":[],"confidence":0.5,"identity_verified":true}`,
		`{"category_id":"cleaning","summary":"x","required_details":[],"confidence":1.1}`,
		`{"category_id":"cleaning","summary":"x","confidence":0.5}`,
		`{"category_id":"cleaning","summary":"x","required_details":[]}`,
		`{"category_id":"cleaning","summary":"x","required_details":[""],"confidence":0.5}`,
		`{"category_id":"cleaning","summary":"x","required_details":[],"confidence":0.5} {"extra":true}`,
	}
	for _,raw:=range cases {t.Run(raw,func(t *testing.T){s,_:=serviceFor(raw);if _,err:=s.Suggest(context.Background(),Request{Market:"OM",Locale:"en",Text:"help"});!errors.Is(err,ErrInvalidSuggestion){t.Fatalf("got %v",err)}})}
}

func TestSuggestRejectsOversizedAndMissingInput(t *testing.T) {
	s,_:=serviceFor(`{"category_id":"cleaning","summary":"x","required_details":[],"confidence":0.5}`)
	if _,err:=s.Suggest(context.Background(),Request{Market:"OM",Locale:"en",Text:strings.Repeat("x",8001)});!errors.Is(err,ErrInvalidRequest){t.Fatalf("got %v",err)}
	if _,err:=s.Suggest(context.Background(),Request{Market:"OM",Locale:"en"});!errors.Is(err,ErrInvalidRequest){t.Fatalf("got %v",err)}
}

func TestSuggestRedactsBeforeModelAndFingerprintsRedactedText(t *testing.T) {
	s,model:=serviceFor(`{"category_id":"cleaning","summary":"x","required_details":[],"confidence":0.5}`)
	s.Redactor=fakeRedactor{text:"cleaned text"}
	got,err:=s.Suggest(context.Background(),Request{Market:"OM",Locale:"en",Text:"phone +968 9000 0000"})
	if err!=nil {t.Fatal(err)}
	if model.input.Text!="cleaned text" {t.Fatalf("model received unredacted text: %q",model.input.Text)}
	if got.Provenance.InputSHA256=="" {t.Fatal("missing redacted-input fingerprint")}
}

func TestSuggestFailsClosedWhenRedactionFails(t *testing.T) {
	s,model:=serviceFor(`{"category_id":"cleaning","summary":"x","required_details":[],"confidence":0.5}`)
	redactionErr:=errors.New("redaction unavailable")
	s.Redactor=fakeRedactor{err:redactionErr}
	_,err:=s.Suggest(context.Background(),Request{Market:"OM",Locale:"en",Text:"private data"})
	if !errors.Is(err,redactionErr) {t.Fatalf("got %v",err)}
	if model.input.Text!="" {t.Fatal("model was called after redaction failed")}
}

func TestSuggestPropagatesCatalogAndModelFailures(t *testing.T) {
	catalogErr:=errors.New("catalog unavailable")
	s,_:=serviceFor(`{"category_id":"cleaning","summary":"x","required_details":[],"confidence":0.5}`)
	s.Catalog=fakeCatalog{err:catalogErr}
	if _,err:=s.Suggest(context.Background(),Request{Market:"OM",Locale:"en",Text:"help"});!errors.Is(err,catalogErr){t.Fatalf("got %v",err)}
	modelErr:=errors.New("model unavailable")
	s,model:=serviceFor(`{}`);model.err=modelErr
	if _,err:=s.Suggest(context.Background(),Request{Market:"OM",Locale:"en",Text:"help"});!errors.Is(err,modelErr){t.Fatalf("got %v",err)}
}

func TestSuggestRejectsEmptyCatalogAndMissingModelProvenance(t *testing.T) {
	s,model:=serviceFor(`{"category_id":"cleaning","summary":"x","required_details":[],"confidence":0.5}`)
	s.Catalog=fakeCatalog{}
	if _,err:=s.Suggest(context.Background(),Request{Market:"OM",Locale:"en",Text:"help"});!errors.Is(err,ErrInvalidRequest){t.Fatalf("got %v",err)}
	s,model=serviceFor(`{"category_id":"cleaning","summary":"x","required_details":[],"confidence":0.5}`)
	model.output.Name=""
	if _,err:=s.Suggest(context.Background(),Request{Market:"OM",Locale:"en",Text:"help"});!errors.Is(err,ErrInvalidSuggestion){t.Fatalf("got %v",err)}
}
