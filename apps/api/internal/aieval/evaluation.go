// Package aieval provides deterministic, offline evaluation gates for AI intake prompts.
package aieval

import (
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
)

var (
	ErrInvalidDataset = errors.New("invalid AI evaluation dataset")
	ErrRegression = errors.New("AI evaluation regression")
)

type Case struct {
	ID string `json:"id"`
	Input string `json:"input"`
	ExpectedCategory string `json:"expected_category"`
	RequiredDetails []string `json:"required_details"`
	Critical bool `json:"critical"`
	RiskTags []string `json:"risk_tags,omitempty"`
}

type Dataset struct {
	Version string `json:"version"`
	PromptID string `json:"prompt_id"`
	PromptVersion string `json:"prompt_version"`
	MinCategoryAccuracy float64 `json:"min_category_accuracy"`
	MinDetailRecall float64 `json:"min_detail_recall"`
	Cases []Case `json:"cases"`
}

type Prediction struct {
	CategoryID string
	RequiredDetails []string
}

type CaseResult struct {
	CaseID string `json:"case_id"`
	CategoryCorrect bool `json:"category_correct"`
	DetailRecall float64 `json:"detail_recall"`
	Critical bool `json:"critical"`
}

type Report struct {
	DatasetVersion string `json:"dataset_version"`
	PromptID string `json:"prompt_id"`
	PromptVersion string `json:"prompt_version"`
	CategoryAccuracy float64 `json:"category_accuracy"`
	DetailRecall float64 `json:"detail_recall"`
	Cases []CaseResult `json:"cases"`
	CriticalFailures []string `json:"critical_failures"`
	Passed bool `json:"passed"`
}

// Evaluate is deterministic and provider-independent. Missing predictions count
// as failures; no model call or network access occurs in this package.
func Evaluate(dataset Dataset, predictions map[string]Prediction) (Report, error) {
	report:=Report{DatasetVersion:dataset.Version,PromptID:dataset.PromptID,PromptVersion:dataset.PromptVersion}
	if err:=validateDataset(dataset);err!=nil{return report,err}
	categoryCorrect,detailMatched,detailExpected:=0,0,0
	seen:=make(map[string]struct{},len(dataset.Cases))
	for _,c:=range dataset.Cases {
		seen[c.ID]=struct{}{}
		prediction,ok:=predictions[c.ID]
		categoryOK:=ok && prediction.CategoryID==c.ExpectedCategory
		if categoryOK {categoryCorrect++}
		matched:=0
		if ok {
			actual:=make(map[string]struct{},len(prediction.RequiredDetails))
			for _,detail:=range prediction.RequiredDetails {actual[normalize(detail)]=struct{}{}}
			for _,required:=range c.RequiredDetails {
				if _,exists:=actual[normalize(required)];exists {matched++}
			}
		}
		detailExpected+=len(c.RequiredDetails)
		detailMatched+=matched
		recall:=1.0
		if len(c.RequiredDetails)>0 {recall=float64(matched)/float64(len(c.RequiredDetails))}
		result:=CaseResult{CaseID:c.ID,CategoryCorrect:categoryOK,DetailRecall:recall,Critical:c.Critical}
		report.Cases=append(report.Cases,result)
		if c.Critical && (!categoryOK || matched!=len(c.RequiredDetails)) {report.CriticalFailures=append(report.CriticalFailures,c.ID)}
	}
	report.CategoryAccuracy=float64(categoryCorrect)/float64(len(dataset.Cases))
	report.DetailRecall=1
	if detailExpected>0 {report.DetailRecall=float64(detailMatched)/float64(detailExpected)}
	sort.Strings(report.CriticalFailures)
	report.Passed=report.CategoryAccuracy>=dataset.MinCategoryAccuracy && report.DetailRecall>=dataset.MinDetailRecall && len(report.CriticalFailures)==0
	if !report.Passed {return report,fmt.Errorf("%w: category accuracy %.3f (min %.3f), detail recall %.3f (min %.3f), critical failures %d",ErrRegression,report.CategoryAccuracy,dataset.MinCategoryAccuracy,report.DetailRecall,dataset.MinDetailRecall,len(report.CriticalFailures))}
	return report,nil
}

var allowedRiskTags = map[string]struct{}{
	"prompt_injection": {},
	"unauthorized_financial_action": {},
	"identity_escalation": {},
	"unauthorized_assignment": {},
	"pii": {},
	"data_exfiltration": {},
	"redaction": {},
	"output_schema_attack": {},
}

func validateDataset(d Dataset) error {
	if strings.TrimSpace(d.Version)==""||strings.TrimSpace(d.PromptID)==""||strings.TrimSpace(d.PromptVersion)==""||len(d.Cases)==0||
		math.IsNaN(d.MinCategoryAccuracy)||math.IsInf(d.MinCategoryAccuracy,0)||d.MinCategoryAccuracy<0||d.MinCategoryAccuracy>1||
		math.IsNaN(d.MinDetailRecall)||math.IsInf(d.MinDetailRecall,0)||d.MinDetailRecall<0||d.MinDetailRecall>1 {
		return ErrInvalidDataset
	}
	seen:=make(map[string]struct{},len(d.Cases))
	for _,c:=range d.Cases {
		if strings.TrimSpace(c.ID)==""||strings.TrimSpace(c.Input)==""||strings.TrimSpace(c.ExpectedCategory)=="" {return ErrInvalidDataset}
		if _,ok:=seen[c.ID];ok{return fmt.Errorf("%w: duplicate case %q",ErrInvalidDataset,c.ID)}
		seen[c.ID]=struct{}{}
		for _,detail:=range c.RequiredDetails {if strings.TrimSpace(detail)=="" {return ErrInvalidDataset}}
		seenTags:=make(map[string]struct{},len(c.RiskTags))
		for _,tag:=range c.RiskTags {
			if _,ok:=allowedRiskTags[tag];!ok{return fmt.Errorf("%w: unknown risk tag %q",ErrInvalidDataset,tag)}
			if _,ok:=seenTags[tag];ok{return fmt.Errorf("%w: duplicate risk tag %q in %s",ErrInvalidDataset,tag,c.ID)}
			seenTags[tag]=struct{}{}
		}
	}
	return nil
}

func normalize(s string) string { return strings.ToLower(strings.Join(strings.Fields(strings.TrimSpace(s))," ")) }
