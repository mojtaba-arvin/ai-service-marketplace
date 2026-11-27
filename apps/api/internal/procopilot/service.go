package procopilot

import (
 "bytes"
 "context"
 "crypto/sha256"
 "encoding/hex"
 "encoding/json"
 "errors"
 "fmt"
 "io"
 "math"
 "strings"
 "time"
)
var(ErrInvalidRequest=errors.New("invalid professional copilot request");ErrInvalidSuggestion=errors.New("model returned an invalid professional suggestion"))
type Request struct{Market,Locale,ServiceCode,Description,EquipmentContext string}
type Evidence struct{ID string;Source string;Text string}
type Knowledge interface{Search(context.Context,string,string,string,string)([]Evidence,error)}
type Redactor interface{Redact(context.Context,string)(string,error)}
type ModelInput struct{Market,Locale,ServiceCode,Description,EquipmentContext string;Evidence []Evidence}
type ModelOutput struct{JSON []byte;Name,Version string}
type Model interface{Generate(context.Context,ModelInput)(ModelOutput,error)}
type EvidenceReference struct{ID string `json:"id"`}
type Provenance struct{Model string `json:"model"`;ModelVersion string `json:"model_version"`;InputSHA256 string `json:"input_sha256"`;EvidenceIDs []string `json:"evidence_ids"`;GeneratedAt time.Time `json:"generated_at"`}
type Suggestion struct{
 DiagnosisSummary string `json:"diagnosis_summary"`
 SafetyWarnings []string `json:"safety_warnings"`
 Steps []string `json:"steps"`
 Checklist []string `json:"checklist"`
 QuoteScopeDraft string `json:"quote_scope_draft"`
 Confidence float64 `json:"confidence"`
 Citations []EvidenceReference `json:"citations"`
 RequiresProfessionalReview bool `json:"requires_professional_review"`
 Provenance Provenance `json:"provenance"`
}
type modelSuggestion struct{
 DiagnosisSummary *string `json:"diagnosis_summary"`
 SafetyWarnings *[]string `json:"safety_warnings"`
 Steps *[]string `json:"steps"`
 Checklist *[]string `json:"checklist"`
 QuoteScopeDraft *string `json:"quote_scope_draft"`
 Confidence *float64 `json:"confidence"`
 Citations *[]EvidenceReference `json:"citations"`
}
type Service struct{Knowledge Knowledge;Model Model;Redactor Redactor;Timeout time.Duration;Now func()time.Time}
func validMetadata(v string)bool{if v==""||strings.TrimSpace(v)!=v||len(v)>128{return false};for _,r:=range v{if r<0x20||r==0x7f{return false}};return true}
func redactBounded(ctx context.Context,r Redactor,v string,max int)(string,error){v=strings.TrimSpace(v);if v==""||len(v)>max{return "",ErrInvalidRequest};out,err:=r.Redact(ctx,v);if err!=nil{return "",fmt.Errorf("redact professional context: %w",err)};out=strings.TrimSpace(out);if out==""||len(out)>max{return "",ErrInvalidRequest};return out,nil}
func (s Service) Suggest(ctx context.Context,req Request)(Suggestion,error){
 if s.Knowledge==nil||s.Model==nil||s.Redactor==nil||s.Timeout<=0||strings.TrimSpace(req.Market)==""||strings.TrimSpace(req.Locale)==""||strings.TrimSpace(req.ServiceCode)==""{return Suggestion{},ErrInvalidRequest}
 description,err:=redactBounded(ctx,s.Redactor,req.Description,8000);if err!=nil{return Suggestion{},err}
 equipment:=strings.TrimSpace(req.EquipmentContext);if equipment!=""{equipment,err=redactBounded(ctx,s.Redactor,equipment,4000);if err!=nil{return Suggestion{},err}}
 retrievalCtx,cancelRetrieval:=context.WithTimeout(ctx,s.Timeout);evidence,err:=s.Knowledge.Search(retrievalCtx,req.Market,req.Locale,req.ServiceCode,description+" "+equipment);cancelRetrieval()
 if err!=nil{return Suggestion{},fmt.Errorf("retrieve professional knowledge: %w",err)};if len(evidence)==0||len(evidence)>12{return Suggestion{},ErrInvalidRequest}
 allowed:=make(map[string]struct{},len(evidence))
 for i:=range evidence{e:=&evidence[i];e.ID=strings.TrimSpace(e.ID);e.Source=strings.TrimSpace(e.Source);if !validMetadata(e.ID)||!validMetadata(e.Source)||len(e.Text)==0||len(e.Text)>4000{return Suggestion{},ErrInvalidRequest};if _,ok:=allowed[e.ID];ok{return Suggestion{},ErrInvalidRequest};allowed[e.ID]=struct{}{};e.Text,err=redactBounded(ctx,s.Redactor,e.Text,4000);if err!=nil{return Suggestion{},fmt.Errorf("redact retrieved evidence: %w",err)}}
 modelCtx,cancel:=context.WithTimeout(ctx,s.Timeout);defer cancel()
 modelInput:=ModelInput{Market:req.Market,Locale:req.Locale,ServiceCode:req.ServiceCode,Description:description,EquipmentContext:equipment,Evidence:evidence}
 output,err:=s.Model.Generate(modelCtx,modelInput)
 if err!=nil{return Suggestion{},fmt.Errorf("generate professional suggestion: %w",err)};if err=modelCtx.Err();err!=nil{return Suggestion{},err}
 if len(output.JSON)==0||len(output.JSON)>32*1024||!validMetadata(output.Name)||!validMetadata(output.Version){return Suggestion{},ErrInvalidSuggestion}
 decoder:=json.NewDecoder(bytes.NewReader(output.JSON));decoder.DisallowUnknownFields();var candidate modelSuggestion
 if err=decoder.Decode(&candidate);err!=nil{return Suggestion{},fmt.Errorf("%w: %v",ErrInvalidSuggestion,err)}
 if err=decoder.Decode(new(any));!errors.Is(err,io.EOF){return Suggestion{},fmt.Errorf("%w: trailing JSON",ErrInvalidSuggestion)}
 if candidate.DiagnosisSummary==nil||candidate.SafetyWarnings==nil||candidate.Steps==nil||candidate.Checklist==nil||candidate.QuoteScopeDraft==nil||candidate.Confidence==nil||candidate.Citations==nil{return Suggestion{},ErrInvalidSuggestion}
 if strings.TrimSpace(*candidate.DiagnosisSummary)==""||len(*candidate.DiagnosisSummary)>1200||strings.TrimSpace(*candidate.QuoteScopeDraft)==""||len(*candidate.QuoteScopeDraft)>1200||len(*candidate.SafetyWarnings)>10||len(*candidate.Steps)==0||len(*candidate.Steps)>12||len(*candidate.Checklist)==0||len(*candidate.Checklist)>12||len(*candidate.Citations)==0||len(*candidate.Citations)>8||math.IsNaN(*candidate.Confidence)||math.IsInf(*candidate.Confidence,0)||*candidate.Confidence<0||*candidate.Confidence>1{return Suggestion{},ErrInvalidSuggestion}
 if err=validateUniqueText(*candidate.SafetyWarnings,240);err!=nil{return Suggestion{},err};if err=validateUniqueText(*candidate.Steps,400);err!=nil{return Suggestion{},err};if err=validateUniqueText(*candidate.Checklist,240);err!=nil{return Suggestion{},err}
 seen:=map[string]struct{}{};citations:=make([]EvidenceReference,0,len(*candidate.Citations))
 for _,citation:=range *candidate.Citations{if _,ok:=allowed[citation.ID];!ok{return Suggestion{},ErrInvalidSuggestion};if _,ok:=seen[citation.ID];ok{return Suggestion{},ErrInvalidSuggestion};seen[citation.ID]=struct{}{};citations=append(citations,citation)}
 fingerprint:=sha256.Sum256([]byte(req.Market+"\n"+req.Locale+"\n"+req.ServiceCode+"\n"+description+"\n"+equipment));evidenceIDs:=make([]string,0,len(citations));for _,c:=range citations{evidenceIDs=append(evidenceIDs,c.ID)}
 now:=time.Now;if s.Now!=nil{now=s.Now}
 return Suggestion{DiagnosisSummary:strings.TrimSpace(*candidate.DiagnosisSummary),SafetyWarnings:*candidate.SafetyWarnings,Steps:*candidate.Steps,Checklist:*candidate.Checklist,QuoteScopeDraft:strings.TrimSpace(*candidate.QuoteScopeDraft),Confidence:*candidate.Confidence,Citations:citations,RequiresProfessionalReview:true,Provenance:Provenance{Model:output.Name,ModelVersion:output.Version,InputSHA256:hex.EncodeToString(fingerprint[:]),EvidenceIDs:evidenceIDs,GeneratedAt:now().UTC()}},nil
}
func validateUniqueText(values []string,max int)error{seen:=map[string]struct{}{};for _,v:=range values{n:=strings.ToLower(strings.Join(strings.Fields(strings.TrimSpace(v))," "));if n==""||len(v)>max{return ErrInvalidSuggestion};if _,ok:=seen[n];ok{return ErrInvalidSuggestion};seen[n]=struct{}{}};return nil}
