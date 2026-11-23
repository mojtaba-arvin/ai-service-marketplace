package disputetriage
import("encoding/json";"errors";"fmt";"os")
var ErrEvaluationRegression=errors.New("dispute triage evaluation regression")
type EvalCase struct{ID string `json:"id"`;RequiredEvidence []string `json:"required_evidence"`;Critical bool `json:"critical"`}
type EvalDataset struct{Version string `json:"version"`;Cases []EvalCase `json:"cases"`}
type EvalReport struct{Version string;EvidenceRecall float64;CriticalFailures []string;Passed bool}
func LoadEvalDataset(path string)(EvalDataset,error){raw,err:=os.ReadFile(path);if err!=nil{return EvalDataset{},err};var d EvalDataset;if err=json.Unmarshal(raw,&d);err!=nil{return EvalDataset{},err};if d.Version==""||len(d.Cases)==0{return EvalDataset{},errors.New("invalid dispute evaluation dataset")};return d,nil}
func Evaluate(d EvalDataset,predictions map[string]Triage,minRecall float64)(EvalReport,error){
 r:=EvalReport{Version:d.Version};if len(d.Cases)==0||minRecall<0||minRecall>1{return r,errors.New("invalid evaluation config")}
 total,hit:=0,0
 for _,c:=range d.Cases{p,ok:=predictions[c.ID];citations:=map[string]struct{}{};for _,o:=range p.ResolutionOptions{for _,ref:=range o.Citations{citations[ref.EvidenceID]=struct{}{}}};caseHit:=0;for _,id:=range c.RequiredEvidence{total++;if _,ok:=citations[id];ok{hit++;caseHit++}};if c.Critical&&(!ok||caseHit!=len(c.RequiredEvidence)||!p.RequiresHumanAdjudication){r.CriticalFailures=append(r.CriticalFailures,c.ID)}}
 if total>0{r.EvidenceRecall=float64(hit)/float64(total)};r.Passed=r.EvidenceRecall>=minRecall&&len(r.CriticalFailures)==0
 if !r.Passed{return r,fmt.Errorf("%w: evidence recall %.3f critical failures %d",ErrEvaluationRegression,r.EvidenceRecall,len(r.CriticalFailures))};return r,nil
}
