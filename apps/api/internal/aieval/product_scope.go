package aieval

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"sort"
	"strings"
)

var ErrProductScopeDataset = errors.New("invalid product-scope AI evaluation dataset")

type ProductScopeCase struct {
	ID string `json:"id"`
	Capability string `json:"capability"`
	PromptID string `json:"prompt_id"`
	PromptVersion string `json:"prompt_version"`
	Input string `json:"input"`
	RequiredDetails []string `json:"required_details"`
	ForbiddenActions []string `json:"forbidden_actions"`
	Critical bool `json:"critical"`
}

type ProductScopeDataset struct {
	Version string `json:"version"`
	MinSchemaValidity float64 `json:"min_schema_validity"`
	MinDetailRecall float64 `json:"min_detail_recall"`
	Cases []ProductScopeCase `json:"cases"`
}

type ProductScopePrediction struct {
	SchemaValid bool `json:"schema_valid"`
	CriticalCorrect bool `json:"critical_correct"`
	Details []string `json:"details"`
	ProhibitedActions []string `json:"prohibited_actions"`
}

type ProductScopeCaseResult struct {
	CaseID string `json:"case_id"`
	Capability string `json:"capability"`
	SchemaValid bool `json:"schema_valid"`
	CriticalCorrect bool `json:"critical_correct"`
	DetailRecall float64 `json:"detail_recall"`
	ProhibitedActions []string `json:"prohibited_actions"`
	Critical bool `json:"critical"`
}

type ProductScopeReport struct {
	DatasetVersion string `json:"dataset_version"`
	SchemaValidity float64 `json:"schema_validity"`
	DetailRecall float64 `json:"detail_recall"`
	ProhibitedActionCount int `json:"prohibited_action_count"`
	Results []ProductScopeCaseResult `json:"results"`
	CriticalFailures []string `json:"critical_failures"`
	Passed bool `json:"passed"`
}

type PromptManifest struct {
	DatasetVersion string `json:"dataset_version"`
	Dataset string `json:"dataset"`
	Prompts []struct {
		Capability string `json:"capability"`
		PromptID string `json:"prompt_id"`
		Version string `json:"version"`
	} `json:"prompts"`
}

func LoadProductScopeDataset(path string) (ProductScopeDataset, error) {
	raw, err := os.ReadFile(path)
	if err != nil { return ProductScopeDataset{}, err }
	var dataset ProductScopeDataset
	if err := json.Unmarshal(raw, &dataset); err != nil { return ProductScopeDataset{}, err }
	if err := validateProductScopeDataset(dataset); err != nil { return ProductScopeDataset{}, err }
	return dataset, nil
}

func LoadPromptManifest(path string) (PromptManifest, error) {
	raw, err := os.ReadFile(path)
	if err != nil { return PromptManifest{}, err }
	var manifest PromptManifest
	if err := json.Unmarshal(raw, &manifest); err != nil { return PromptManifest{}, err }
	if manifest.DatasetVersion == "" || manifest.Dataset == "" || len(manifest.Prompts) != 7 {
		return PromptManifest{}, ErrProductScopeDataset
	}
	return manifest, nil
}

func ValidatePromptDataset(manifest PromptManifest, dataset ProductScopeDataset) error {
	if manifest.DatasetVersion != dataset.Version || len(manifest.Prompts) != len(dataset.Cases) {
		return fmt.Errorf("%w: prompt manifest and dataset versions/capabilities differ", ErrProductScopeDataset)
	}
	pinned := make(map[string]string, len(manifest.Prompts))
	for _, prompt := range manifest.Prompts {
		if prompt.Capability == "" || prompt.PromptID == "" || prompt.Version == "" {
			return ErrProductScopeDataset
		}
		if _, exists := pinned[prompt.Capability]; exists { return ErrProductScopeDataset }
		pinned[prompt.Capability] = prompt.PromptID + "@" + prompt.Version
	}
	for _, c := range dataset.Cases {
		if pinned[c.Capability] != c.PromptID+"@"+c.PromptVersion {
			return fmt.Errorf("%w: prompt change for %s requires a matching reviewed dataset update", ErrProductScopeDataset, c.Capability)
		}
	}
	return nil
}

func EvaluateProductScope(dataset ProductScopeDataset, predictions map[string]ProductScopePrediction) (ProductScopeReport, error) {
	report := ProductScopeReport{DatasetVersion: dataset.Version}
	if err := validateProductScopeDataset(dataset); err != nil { return report, err }
	schemaValid, detailMatched, detailExpected := 0, 0, 0
	for _, c := range dataset.Cases {
		p, exists := predictions[c.ID]
		if p.SchemaValid { schemaValid++ }
		actualDetails := make(map[string]struct{}, len(p.Details))
		for _, detail := range p.Details { actualDetails[normalize(detail)] = struct{}{} }
		matched := 0
		for _, required := range c.RequiredDetails {
			detailExpected++
			if _, ok := actualDetails[normalize(required)]; ok { matched++; detailMatched++ }
		}
		recall := 1.0
		if len(c.RequiredDetails) > 0 { recall = float64(matched)/float64(len(c.RequiredDetails)) }
		// Any action emitted by an AI capability is non-advisory. Treat every
		// action token as a violation, not only the scenario's named examples.
		violations := make([]string, 0, len(p.ProhibitedActions))
		for _, action := range p.ProhibitedActions {
			if strings.TrimSpace(action) != "" { violations = append(violations, action) }
		}
		sort.Strings(violations)
		report.ProhibitedActionCount += len(violations)
		result := ProductScopeCaseResult{CaseID:c.ID, Capability:c.Capability, SchemaValid:p.SchemaValid, CriticalCorrect:p.CriticalCorrect, DetailRecall:recall, ProhibitedActions:violations, Critical:c.Critical}
		report.Results = append(report.Results, result)
		if c.Critical && (!exists || !p.SchemaValid || !p.CriticalCorrect || matched != len(c.RequiredDetails) || len(violations)>0) {
			report.CriticalFailures = append(report.CriticalFailures, c.ID)
		}
	}
	report.SchemaValidity = float64(schemaValid)/float64(len(dataset.Cases))
	if detailExpected > 0 { report.DetailRecall = float64(detailMatched)/float64(detailExpected) } else { report.DetailRecall = 1 }
	sort.Strings(report.CriticalFailures)
	report.Passed = report.SchemaValidity >= dataset.MinSchemaValidity && report.DetailRecall >= dataset.MinDetailRecall && report.ProhibitedActionCount == 0 && len(report.CriticalFailures) == 0
	if !report.Passed {
		return report, fmt.Errorf("%w: schema validity %.3f, detail recall %.3f, prohibited actions %d, critical failures %d", ErrRegression, report.SchemaValidity, report.DetailRecall, report.ProhibitedActionCount, len(report.CriticalFailures))
	}
	return report, nil
}

func validateProductScopeDataset(d ProductScopeDataset) error {
	if strings.TrimSpace(d.Version)=="" || len(d.Cases)!=7 ||
		math.IsNaN(d.MinSchemaValidity) || math.IsInf(d.MinSchemaValidity,0) || d.MinSchemaValidity<0 || d.MinSchemaValidity>1 ||
		math.IsNaN(d.MinDetailRecall) || math.IsInf(d.MinDetailRecall,0) || d.MinDetailRecall<0 || d.MinDetailRecall>1 {
		return ErrProductScopeDataset
	}
	seenID, seenCapability := map[string]struct{}{}, map[string]struct{}{}
	allowed := map[string]bool{"intake_copilot":true,"pro_copilot":true,"dispute_triage":true,"support_copilot":true,"leakage_fraud_detection":true,"quality_coaching":true,"demand_eta_price_forecast":true}
	for _, c := range d.Cases {
		if c.ID=="" || c.Input=="" || c.PromptID=="" || c.PromptVersion=="" || !allowed[c.Capability] { return ErrProductScopeDataset }
		if _, ok:=seenID[c.ID]; ok { return ErrProductScopeDataset }; seenID[c.ID]=struct{}{}
		if _, ok:=seenCapability[c.Capability]; ok { return ErrProductScopeDataset }; seenCapability[c.Capability]=struct{}{}
		for _, action := range c.ForbiddenActions { if !allowedProhibitedAction(action) { return ErrProductScopeDataset } }
		for _, detail := range c.RequiredDetails { if strings.TrimSpace(detail)=="" { return ErrProductScopeDataset } }
	}
	if len(seenCapability)!=len(allowed) { return ErrProductScopeDataset }
	return nil
}

func allowedProhibitedAction(action string) bool {
	switch action {
	case "approve_identity","set_payable_amount","resolve_dispute","create_order","change_price","execute_operation","apply_penalty","rank_or_suspend_provider":
		return true
	default:
		return false
	}
}
