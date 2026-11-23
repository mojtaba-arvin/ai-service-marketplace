package disputetriage

import(
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
var(ErrInvalidCase=errors.New("invalid dispute triage request");ErrInvalidTriage=errors.New("invalid dispute triage output"))
type Request struct{Market,Locale,DisputeType,Reason string}
type Evidence struct{ID,Source,Text string}
type EvidenceSource interface{Retrieve(context.Context,string,string,string,string)([]Evidence,error)}
type Redactor interface{Redact(context.Context,string)(string,error)}
type ModelInput struct{Market,Locale,DisputeType,Reason string;Evidence []Evidence}
type ModelOutput struct{JSON []byte;Name,Version string}
type Model interface{Generate(context.Context,ModelInput)(ModelOutput,error)}
type Citation struct{EvidenceID string `json:"evidence_id"`}
type ResolutionOption struct{Description string `json:"description"`;Citations []Citation `json:"citations"`}
type Provenance struct{Model string `json:"model"`;Version string `json:"version"`;InputSHA256 string `json:"input_sha256"`;EvidenceIDs []string `json:"evidence_ids"`;GeneratedAt time.Time `json:"generated_at"`}
type Triage struct{
 CaseSummary string `json:"case_summary"`
 EstablishedFacts []string `json:"established_facts"`
 MissingInformation []string `json:"missing_information"`
 ResolutionOptions []ResolutionOption `json:"resolution_options"`
 Confidence float64 `json:"confidence"`
 RequiresHumanAdjudication bool `json:"requires_human_adjudication"`
 Provenance Provenance `json:"provenance"`
}
type modelOutput struct{
 CaseSummary *string `json:"case_summary"`
 EstablishedFacts *[]string `json:"established_facts"`
 MissingInformation *[]string `json:"missing_information"`
 ResolutionOptions *[]ResolutionOption `json:"resolution_options"`
 Confidence *float64 `json:"confidence"`
}
type Service struct{Evidence EvidenceSource;Redactor Redactor;Model Model;Timeout time.Duration;Now func()time.Time}
func boundedRedaction(ctx context.Context,r Redactor,value string,max int)(string,error){
 value=strings.TrimSpace(value);if value==""||len(value)>max{return "",ErrInvalidCase}
 safe,err:=r.Redact(ctx,value);if err!=nil{return "",fmt.Errorf("redact dispute evidence: %w",err)}
 safe=strings.TrimSpace(safe);if safe==""||len(safe)>max{return "",ErrInvalidCase};return safe,nil
}
func (s Service) Triage(ctx context.Context,req Request)(Triage,error){
 if s.Evidence==nil||s.Redactor==nil||s.Model==nil||s.Timeout<=0||strings.TrimSpace(req.Market)==""||strings.TrimSpace(req.Locale)==""||strings.TrimSpace(req.DisputeType)==""{return Triage{},ErrInvalidCase}
 reason,err:=boundedRedaction(ctx,s.Redactor,req.Reason,6000);if err!=nil{return Triage{},err}
 retrievalCtx,cancelRetrieval:=context.WithTimeout(ctx,s.Timeout)
 evidence,err:=s.Evidence.Retrieve(retrievalCtx,req.Market,req.Locale,req.DisputeType,reason);cancelRetrieval()
 if err!=nil{return Triage{},fmt.Errorf("retrieve dispute policy and evidence: %w",err)}
 if len(evidence)==0||len(evidence)>16{return Triage{},ErrInvalidCase}
 allowed:=map[string]struct{}{}
 for i:=range evidence{e:=&evidence[i];e.ID=strings.TrimSpace(e.ID);e.Source=strings.TrimSpace(e.Source);if e.ID==""||e.Source==""||len(e.ID)>128||len(e.Source)>128||len(e.Text)==0||len(e.Text)>4000{return Triage{},ErrInvalidCase};if _,ok:=allowed[e.ID];ok{return Triage{},ErrInvalidCase};allowed[e.ID]=struct{}{};e.Text,err=boundedRedaction(ctx,s.Redactor,e.Text,4000);if err!=nil{return Triage{},err}}
 modelCtx,cancel:=context.WithTimeout(ctx,s.Timeout);defer cancel()
 output,err:=s.Model.Generate(modelCtx,ModelInput{Market:req.Market,Locale:req.Locale,DisputeType:req.DisputeType,Reason:reason,Evidence:evidence})
 if err!=nil{return Triage{},fmt.Errorf("generate dispute triage: %w",err)};if err=modelCtx.Err();err!=nil{return Triage{},err}
 if len(output.JSON)==0||len(output.JSON)>32*1024||strings.TrimSpace(output.Name)==""||strings.TrimSpace(output.Version)==""{return Triage{},ErrInvalidTriage}
 dec:=json.NewDecoder(bytes.NewReader(output.JSON));dec.DisallowUnknownFields();var candidate modelOutput
 if err=dec.Decode(&candidate);err!=nil{return Triage{},fmt.Errorf("%w: %v",ErrInvalidTriage,err)}
 if err=dec.Decode(new(any));!errors.Is(err,io.EOF){return Triage{},ErrInvalidTriage}
 if candidate.CaseSummary==nil||candidate.EstablishedFacts==nil||candidate.MissingInformation==nil||candidate.ResolutionOptions==nil||candidate.Confidence==nil{return Triage{},ErrInvalidTriage}
 if strings.TrimSpace(*candidate.CaseSummary)==""||len(*candidate.CaseSummary)>2000||len(*candidate.EstablishedFacts)>12||len(*candidate.MissingInformation)>12||len(*candidate.ResolutionOptions)==0||len(*candidate.ResolutionOptions)>5||math.IsNaN(*candidate.Confidence)||math.IsInf(*candidate.Confidence,0)||*candidate.Confidence<0||*candidate.Confidence>1{return Triage{},ErrInvalidTriage}
 if err=validateLines(*candidate.EstablishedFacts,400);err!=nil{return Triage{},err};if err=validateLines(*candidate.MissingInformation,300);err!=nil{return Triage{},err}
 options:=make([]ResolutionOption,0,len(*candidate.ResolutionOptions));used:=map[string]struct{}{}
 for _,option:=range *candidate.ResolutionOptions{if strings.TrimSpace(option.Description)==""||len(option.Description)>600||len(option.Citations)==0||len(option.Citations)>6{return Triage{},ErrInvalidTriage};for _,citation:=range option.Citations{if _,ok:=allowed[citation.EvidenceID];!ok{return Triage{},ErrInvalidTriage};used[citation.EvidenceID]=struct{}{}};options=append(options,option)}
 factsEvidence:=make([]string,0,len(used));for id:=range used{factsEvidence=append(factsEvidence,id)}
 fingerprint:=sha256.Sum256([]byte(req.Market+"\n"+req.Locale+"\n"+req.DisputeType+"\n"+reason))
 now:=time.Now;if s.Now!=nil{now=s.Now}
 return Triage{CaseSummary:strings.TrimSpace(*candidate.CaseSummary),EstablishedFacts:*candidate.EstablishedFacts,MissingInformation:*candidate.MissingInformation,ResolutionOptions:options,Confidence:*candidate.Confidence,RequiresHumanAdjudication:true,Provenance:Provenance{Model:output.Name,Version:output.Version,InputSHA256:hex.EncodeToString(fingerprint[:]),EvidenceIDs:factsEvidence,GeneratedAt:now().UTC()}},nil
}
func validateLines(values []string,max int)error{seen:=map[string]struct{}{};for _,v:=range values{n:=strings.ToLower(strings.Join(strings.Fields(strings.TrimSpace(v))," "));if n==""||len(v)>max{return ErrInvalidTriage};if _,ok:=seen[n];ok{return ErrInvalidTriage};seen[n]=struct{}{}};return nil}
