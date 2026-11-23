package procopilot

import (
 "encoding/json"
 "errors"
 "fmt"
 "os"
 "strings"
)
var ErrEvaluationRegression=errors.New("professional copilot evaluation regression")
type EvaluationCase struct{ID string `json:"id"`;RequiredChecklist []string `json:"required_checklist"`;RequiredCitations []string `json:"required_citations"`;Critical bool `json:"critical"`}
type EvaluationDataset struct{Version string `json:"version"`;Cases []EvaluationCase `json:"cases"`}
type EvaluationReport struct{Version string;ChecklistRecall,CitationRecall float64;CriticalFailures []string;Passed bool}
func LoadEvaluationDataset(path string)(EvaluationDataset,error){
 raw,err:=os.ReadFile(path);if err!=nil{return EvaluationDataset{},err};var d EvaluationDataset
 if err=json.Unmarshal(raw,&d);err!=nil{return EvaluationDataset{},err}
 if strings.TrimSpace(d.Version)==""||len(d.Cases)==0{return EvaluationDataset{},errors.New("invalid professional evaluation dataset")}
 seen:=map[string]struct{}{};for _,c:=range d.Cases{if strings.TrimSpace(c.ID)==""{return EvaluationDataset{},errors.New("invalid evaluation case")};if _,ok:=seen[c.ID];ok{return EvaluationDataset{},errors.New("duplicate evaluation case")};seen[c.ID]=struct{}{}}
 return d,nil
}
func Evaluate(d EvaluationDataset,predictions map[string]Suggestion,minChecklist,minCitations float64)(EvaluationReport,error){
 r:=EvaluationReport{Version:d.Version};if len(d.Cases)==0||minChecklist<0||minChecklist>1||minCitations<0||minCitations>1{return r,errors.New("invalid evaluation configuration")}
 checklistTotal,citationTotal,checklistHit,citationHit:=0,0,0,0
 for _,c:=range d.Cases{
  p,ok:=predictions[c.ID];check:=map[string]struct{}{};for _,v:=range p.Checklist{check[normalizeEval(v)]=struct{}{}}
  cites:=map[string]struct{}{};for _,v:=range p.Citations{cites[v.ID]=struct{}{}}
  cm,em:=0,0
  for _,v:=range c.RequiredChecklist{checklistTotal++;if _,ok:=check[normalizeEval(v)];ok{checklistHit++;cm++}}
  for _,v:=range c.RequiredCitations{citationTotal++;if _,ok:=cites[v];ok{citationHit++;em++}}
  if c.Critical&&(!ok||cm!=len(c.RequiredChecklist)||em!=len(c.RequiredCitations)||!p.RequiresProfessionalReview){r.CriticalFailures=append(r.CriticalFailures,c.ID)}
 }
 if checklistTotal>0{r.ChecklistRecall=float64(checklistHit)/float64(checklistTotal)}
 if citationTotal>0{r.CitationRecall=float64(citationHit)/float64(citationTotal)}
 r.Passed=r.ChecklistRecall>=minChecklist&&r.CitationRecall>=minCitations&&len(r.CriticalFailures)==0
 if !r.Passed{return r,fmt.Errorf("%w: checklist recall %.3f citation recall %.3f critical failures %d",ErrEvaluationRegression,r.ChecklistRecall,r.CitationRecall,len(r.CriticalFailures))}
 return r,nil
}
func normalizeEval(v string)string{return strings.ToLower(strings.Join(strings.Fields(strings.TrimSpace(v))," "))}
