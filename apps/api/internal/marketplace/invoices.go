package marketplace

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/big"
	"sort"
	"strings"
	"time"
)

var (
	ErrInvoiceInvalid = errors.New("invalid invoice snapshot")
	ErrInvoiceSourceMismatch = errors.New("invoice source order, price, and ledger evidence do not reconcile")
	ErrInvoiceTaxConfigurationMissing = errors.New("explicit market tax configuration is missing")
	ErrInvoiceTaxConfigurationAmbiguous = errors.New("market tax configuration is ambiguous")
	ErrInvoiceTaxConfigurationInvalid = errors.New("market tax configuration is invalid or not approved")
	ErrInvoiceTaxOverflow = errors.New("invoice tax calculation overflow")
	ErrInvoiceIdempotencyConflict = errors.New("invoice idempotency key reused with different content")
)

type InvoiceRoundingMode string

const (
	InvoiceRoundHalfUp InvoiceRoundingMode = "half_up"
	InvoiceRoundHalfEven InvoiceRoundingMode = "half_even"
	InvoiceRoundDown InvoiceRoundingMode = "down"
	InvoiceRoundUp InvoiceRoundingMode = "up"
)

// InvoiceTaxRule is an immutable, adopter-supplied tax rule revision. Approval
// fields are provenance only: callers must resolve this record from a
// persistence boundary that has already enforced the required authorization.
type InvoiceTaxRule struct {
	ID                string    `json:"id"`
	Version           int64     `json:"version"`
	MarketID          string    `json:"market_id"`
	JurisdictionCode  string    `json:"jurisdiction_code"`
	TaxCategory       string    `json:"tax_category"`
	ComponentCode     string    `json:"component_code"`
	Currency          string    `json:"currency"`
	RateNumerator     int64     `json:"rate_numerator"`
	RateDenominator   int64     `json:"rate_denominator"`
	RoundingMode      InvoiceRoundingMode `json:"rounding_mode"`
	EffectiveFrom     time.Time `json:"effective_from"`
	EffectiveUntil    *time.Time `json:"effective_until,omitempty"`
	ApprovedBy        string    `json:"approved_by"`
	ApprovalReference string    `json:"approval_reference"`
	ApprovedAt        time.Time `json:"approved_at"`
}

type InvoiceTaxInput struct {
	MarketID         string    `json:"market_id"`
	JurisdictionCode string    `json:"jurisdiction_code"`
	TaxCategory      string    `json:"tax_category"`
	ComponentCode    string    `json:"component_code"`
	Currency         string    `json:"currency"`
	TaxableMinor     int64     `json:"taxable_minor"`
	CalculatedAt     time.Time `json:"calculated_at"`
}

type InvoiceTaxComponent struct {
	RuleID            string    `json:"rule_id"`
	RuleVersion       int64     `json:"rule_version"`
	MarketID          string    `json:"market_id"`
	JurisdictionCode  string    `json:"jurisdiction_code"`
	TaxCategory       string    `json:"tax_category"`
	ComponentCode     string    `json:"component_code"`
	Currency          string    `json:"currency"`
	TaxableMinor      int64     `json:"taxable_minor"`
	RateNumerator     int64     `json:"rate_numerator"`
	RateDenominator   int64     `json:"rate_denominator"`
	RoundingMode      InvoiceRoundingMode `json:"rounding_mode"`
	EffectiveFrom     time.Time `json:"effective_from"`
	EffectiveUntil    *time.Time `json:"effective_until,omitempty"`
	ApprovedBy        string    `json:"approved_by"`
	ApprovalReference string    `json:"approval_reference"`
	ApprovedAt        time.Time `json:"approved_at"`
	TaxMinor          int64     `json:"tax_minor"`
	CalculatedAt      time.Time `json:"calculated_at"`
}

type InvoiceLineInput struct {
	SourceLineID string `json:"source_line_id"`
	Position     int    `json:"position"`
	Description  string `json:"description"`
	Quantity     int64  `json:"quantity"`
	UnitMinor    int64  `json:"unit_minor"`
	Currency     string `json:"currency"`
}

type InvoiceLine struct {
	SourceLineID string `json:"source_line_id"`
	Position     int    `json:"position"`
	Description  string `json:"description"`
	Quantity     int64  `json:"quantity"`
	UnitMinor    int64  `json:"unit_minor"`
	LineMinor    int64  `json:"line_minor"`
	Currency     string `json:"currency"`
}

// InvoiceSourceSnapshot must be assembled from the authorized order/price
// snapshot and an explicitly classified billable-ledger result. This package
// intentionally does not infer ledger semantics from debit/credit totals.
type InvoiceSourceSnapshot struct {
	TenantID                  string             `json:"tenant_id"`
	CustomerReference         string             `json:"customer_reference"`
	OrderID                   string             `json:"order_id"`
	PriceVersionID            string             `json:"price_version_id"`
	OrderAmountMinor          int64              `json:"order_amount_minor"`
	OrderCurrency             string             `json:"order_currency"`
	PriceAmountMinor          int64              `json:"price_amount_minor"`
	PriceCurrency             string             `json:"price_currency"`
	LedgerBillableAmountMinor int64              `json:"ledger_billable_amount_minor"`
	LedgerCurrency            string             `json:"ledger_currency"`
	LedgerSemantics           string             `json:"ledger_semantics"`
	LedgerJournalIDs          []string           `json:"ledger_journal_ids"`
	PaymentOperationIDs       []string           `json:"payment_operation_ids,omitempty"`
	Lines                     []InvoiceLineInput `json:"lines"`
}

type InvoiceBuildInput struct {
	ID                      string                `json:"id"`
	IdempotencyKey          string                `json:"idempotency_key"`
	Version                 int64                 `json:"version"`
	IssuedAt                time.Time             `json:"issued_at"`
	IssuerReference         string                `json:"issuer_reference"`
	Source                  InvoiceSourceSnapshot `json:"source"`
	TaxInputs               []InvoiceTaxInput     `json:"tax_inputs"`
	TaxRules                []InvoiceTaxRule      `json:"tax_rules"`
	CorrectionOfID           string                `json:"correction_of_id,omitempty"`
	CorrectionReason         string                `json:"correction_reason,omitempty"`
	FinancialCorrection      bool                  `json:"financial_correction,omitempty"`
	CompensatingJournalIDs   []string              `json:"compensating_journal_ids,omitempty"`
}

type InvoiceRecord struct {
	ID                    string                `json:"id"`
	IdempotencyKey        string                `json:"idempotency_key"`
	Version               int64                 `json:"version"`
	Status                string                `json:"status"`
	IssuedAt              time.Time             `json:"issued_at"`
	IssuerReference       string                `json:"issuer_reference"`
	TenantID              string                `json:"tenant_id"`
	CustomerReference     string                `json:"customer_reference"`
	SourceOrderID         string                `json:"source_order_id"`
	PriceVersionID        string                `json:"price_version_id"`
	Currency              string                `json:"currency"`
	Lines                 []InvoiceLine         `json:"lines"`
	SubtotalMinor         int64                 `json:"subtotal_minor"`
	TaxComponents         []InvoiceTaxComponent `json:"tax_components"`
	TaxTotalMinor         int64                 `json:"tax_total_minor"`
	TotalMinor            int64                 `json:"total_minor"`
	LedgerSemantics       string                `json:"ledger_semantics"`
	LedgerJournalIDs      []string              `json:"ledger_journal_ids"`
	PaymentOperationIDs   []string              `json:"payment_operation_ids,omitempty"`
	CorrectionOfID        string                `json:"correction_of_id,omitempty"`
	CorrectionReason      string                `json:"correction_reason,omitempty"`
	FinancialCorrection   bool                  `json:"financial_correction,omitempty"`
	CompensatingJournalIDs []string             `json:"compensating_journal_ids,omitempty"`
	RequestHash           string                `json:"request_hash"`
}

func normalizeInvoiceCurrency(currency string) (string, error) {
	currency = strings.ToUpper(strings.TrimSpace(currency))
	if ValidateAmount(1, currency) != nil {
		return "", ErrInvoiceInvalid
	}
	return currency, nil
}

func validInvoiceRounding(mode InvoiceRoundingMode) bool {
	switch mode {
	case InvoiceRoundHalfUp, InvoiceRoundHalfEven, InvoiceRoundDown, InvoiceRoundUp:
		return true
	default:
		return false
	}
}

func validateInvoiceTaxRule(rule InvoiceTaxRule) error {
	currency, err := normalizeInvoiceCurrency(rule.Currency)
	if err != nil || currency != rule.Currency || rule.ID == "" || rule.Version < 1 ||
		strings.TrimSpace(rule.MarketID) == "" || strings.TrimSpace(rule.JurisdictionCode) == "" ||
		strings.TrimSpace(rule.TaxCategory) == "" || strings.TrimSpace(rule.ComponentCode) == "" ||
		rule.RateNumerator < 0 || rule.RateDenominator <= 0 || !validInvoiceRounding(rule.RoundingMode) ||
		rule.EffectiveFrom.IsZero() || rule.ApprovedAt.IsZero() ||
		strings.TrimSpace(rule.ApprovedBy) == "" || strings.TrimSpace(rule.ApprovalReference) == "" {
		return ErrInvoiceTaxConfigurationInvalid
	}
	if rule.EffectiveUntil != nil && !rule.EffectiveUntil.After(rule.EffectiveFrom) {
		return ErrInvoiceTaxConfigurationInvalid
	}
	return nil
}

func invoiceRuleMatches(rule InvoiceTaxRule, input InvoiceTaxInput) bool {
	if rule.MarketID != input.MarketID || rule.JurisdictionCode != input.JurisdictionCode ||
		rule.TaxCategory != input.TaxCategory || rule.ComponentCode != input.ComponentCode ||
		rule.Currency != input.Currency || input.CalculatedAt.Before(rule.EffectiveFrom) {
		return false
	}
	return rule.EffectiveUntil == nil || input.CalculatedAt.Before(*rule.EffectiveUntil)
}

func roundInvoiceTax(numerator, denominator *big.Int, mode InvoiceRoundingMode) (*big.Int, error) {
	if denominator.Sign() <= 0 || numerator.Sign() < 0 {
		return nil, ErrInvoiceTaxConfigurationInvalid
	}
	q, r := new(big.Int), new(big.Int)
	q.QuoRem(numerator, denominator, r)
	if r.Sign() == 0 || mode == InvoiceRoundDown {
		return q, nil
	}
	switch mode {
	case InvoiceRoundUp:
		q.Add(q, big.NewInt(1))
	case InvoiceRoundHalfUp, InvoiceRoundHalfEven:
		twiceRemainder := new(big.Int).Lsh(new(big.Int).Set(r), 1)
		comparison := twiceRemainder.Cmp(denominator)
		if comparison > 0 || (comparison == 0 && (mode == InvoiceRoundHalfUp || q.Bit(0) == 1)) {
			q.Add(q, big.NewInt(1))
		}
	default:
		return nil, ErrInvoiceTaxConfigurationInvalid
	}
	return q, nil
}

func calculateInvoiceTax(input InvoiceTaxInput, rule InvoiceTaxRule) (InvoiceTaxComponent, error) {
	if err := validateInvoiceTaxRule(rule); err != nil {
		return InvoiceTaxComponent{}, err
	}
	currency, err := normalizeInvoiceCurrency(input.Currency)
	if err != nil || currency != input.Currency || input.TaxableMinor < 0 || input.CalculatedAt.IsZero() ||
		strings.TrimSpace(input.MarketID) == "" || strings.TrimSpace(input.JurisdictionCode) == "" ||
		strings.TrimSpace(input.TaxCategory) == "" || strings.TrimSpace(input.ComponentCode) == "" ||
		!invoiceRuleMatches(rule, input) || rule.ApprovedAt.After(input.CalculatedAt) {
		return InvoiceTaxComponent{}, ErrInvoiceTaxConfigurationInvalid
	}
	product := new(big.Int).Mul(big.NewInt(input.TaxableMinor), big.NewInt(rule.RateNumerator))
	tax, err := roundInvoiceTax(product, big.NewInt(rule.RateDenominator), rule.RoundingMode)
	if err != nil {
		return InvoiceTaxComponent{}, err
	}
	if !tax.IsInt64() {
		return InvoiceTaxComponent{}, ErrInvoiceTaxOverflow
	}
	return InvoiceTaxComponent{
		RuleID: rule.ID, RuleVersion: rule.Version, MarketID: rule.MarketID,
		JurisdictionCode: rule.JurisdictionCode, TaxCategory: rule.TaxCategory,
		ComponentCode: rule.ComponentCode, Currency: rule.Currency,
		TaxableMinor: input.TaxableMinor, RateNumerator: rule.RateNumerator,
		RateDenominator: rule.RateDenominator, RoundingMode: rule.RoundingMode,
		EffectiveFrom: rule.EffectiveFrom, EffectiveUntil: cloneInvoiceTime(rule.EffectiveUntil),
		ApprovedBy: rule.ApprovedBy, ApprovalReference: rule.ApprovalReference,
		ApprovedAt: rule.ApprovedAt, TaxMinor: tax.Int64(), CalculatedAt: input.CalculatedAt.UTC(),
	}, nil
}

func cloneInvoiceTime(value *time.Time) *time.Time {
	if value == nil { return nil }
	copy := *value
	return &copy
}

func calculateInvoiceTaxes(inputs []InvoiceTaxInput, rules []InvoiceTaxRule) ([]InvoiceTaxComponent, int64, error) {
	if len(inputs) == 0 || len(rules) == 0 {
		return nil, 0, ErrInvoiceTaxConfigurationMissing
	}
	components := make([]InvoiceTaxComponent, 0, len(inputs))
	seen := make(map[string]bool, len(inputs))
	var total int64
	for _, input := range inputs {
		if seen[input.ComponentCode] { return nil, 0, ErrInvoiceTaxConfigurationAmbiguous }
		seen[input.ComponentCode] = true
		matches := make([]InvoiceTaxRule, 0, 1)
		for _, rule := range rules {
			if invoiceRuleMatches(rule, input) { matches = append(matches, rule) }
		}
		if len(matches) == 0 { return nil, 0, ErrInvoiceTaxConfigurationMissing }
		if len(matches) != 1 { return nil, 0, ErrInvoiceTaxConfigurationAmbiguous }
		component, err := calculateInvoiceTax(input, matches[0])
		if err != nil { return nil, 0, err }
		if total > math.MaxInt64-component.TaxMinor { return nil, 0, ErrInvoiceTaxOverflow }
		total += component.TaxMinor
		components = append(components, component)
	}
	return components, total, nil
}

func uniqueInvoiceReferences(values []string, required bool) ([]string, error) {
	if required && len(values) == 0 { return nil, ErrInvoiceSourceMismatch }
	result := append([]string(nil), values...)
	seen := make(map[string]bool, len(result))
	for _, value := range result {
		value = strings.TrimSpace(value)
		if value == "" || value != strings.TrimSpace(value) || len(value) > 200 || seen[value] { return nil, ErrInvoiceSourceMismatch }
		seen[value] = true
	}
	return result, nil
}

func buildInvoiceLines(inputs []InvoiceLineInput, currency string) ([]InvoiceLine, int64, error) {
	if len(inputs) == 0 || len(inputs) > 100 { return nil, 0, ErrInvoiceInvalid }
	lines := make([]InvoiceLine, 0, len(inputs))
	var subtotal int64
	lastPosition := -1
	for _, input := range inputs {
		if strings.TrimSpace(input.SourceLineID) == "" || input.SourceLineID != strings.TrimSpace(input.SourceLineID) || strings.TrimSpace(input.Description) == "" ||
			input.Quantity <= 0 || input.UnitMinor <= 0 || input.Position <= lastPosition ||
			input.Currency != currency {
			return nil, 0, ErrInvoiceInvalid
		}
		lastPosition = input.Position
		lineAmount := new(big.Int).Mul(big.NewInt(input.Quantity), big.NewInt(input.UnitMinor))
		if !lineAmount.IsInt64() || lineAmount.Sign() <= 0 { return nil, 0, ErrInvoiceTaxOverflow }
		amount := lineAmount.Int64()
		if subtotal > math.MaxInt64-amount { return nil, 0, ErrInvoiceTaxOverflow }
		subtotal += amount
		lines = append(lines, InvoiceLine{
			SourceLineID: input.SourceLineID, Position: input.Position, Description: input.Description,
			Quantity: input.Quantity, UnitMinor: input.UnitMinor, LineMinor: amount, Currency: currency,
		})
	}
	return lines, subtotal, nil
}

func invoiceRequestHash(record InvoiceRecord) (string, error) {
	record.RequestHash = ""
	payload, err := json.Marshal(record)
	if err != nil { return "", err }
	hash := sha256.Sum256(payload)
	return hex.EncodeToString(hash[:]), nil
}

// BuildInvoiceRecord creates an immutable in-memory issue snapshot. Persistence,
// tenant authorization, source attribution and tax-rule approval must be
// enforced by the owning transactional Store boundary before this is called.
func BuildInvoiceRecord(input InvoiceBuildInput) (InvoiceRecord, error) {
	currency, err := normalizeInvoiceCurrency(input.Source.OrderCurrency)
	if err != nil || input.ID == "" || input.ID != strings.TrimSpace(input.ID) || strings.TrimSpace(input.IdempotencyKey) == "" || input.IdempotencyKey != strings.TrimSpace(input.IdempotencyKey) ||
		len(input.IdempotencyKey) > 200 || input.Version < 1 || input.IssuedAt.IsZero() ||
		strings.TrimSpace(input.IssuerReference) == "" || strings.TrimSpace(input.Source.TenantID) == "" ||
		strings.TrimSpace(input.Source.CustomerReference) == "" || strings.TrimSpace(input.Source.OrderID) == "" ||
		strings.TrimSpace(input.Source.PriceVersionID) == "" ||
		input.Source.OrderAmountMinor <= 0 || input.Source.PriceAmountMinor <= 0 ||
		input.Source.LedgerBillableAmountMinor <= 0 || strings.TrimSpace(input.Source.LedgerSemantics) == "" {
		return InvoiceRecord{}, ErrInvoiceInvalid
	}
	if input.Source.OrderCurrency != currency || input.Source.PriceCurrency != currency ||
		input.Source.LedgerCurrency != currency ||
		input.Source.OrderAmountMinor != input.Source.PriceAmountMinor ||
		input.Source.OrderAmountMinor != input.Source.LedgerBillableAmountMinor {
		return InvoiceRecord{}, ErrInvoiceSourceMismatch
	}
	journals, err := uniqueInvoiceReferences(input.Source.LedgerJournalIDs, true)
	if err != nil { return InvoiceRecord{}, err }
	payments, err := uniqueInvoiceReferences(input.Source.PaymentOperationIDs, false)
	if err != nil { return InvoiceRecord{}, err }
	lines, subtotal, err := buildInvoiceLines(input.Source.Lines, currency)
	if err != nil { return InvoiceRecord{}, err }
	if subtotal != input.Source.OrderAmountMinor { return InvoiceRecord{}, ErrInvoiceSourceMismatch }
	taxInputs := append([]InvoiceTaxInput(nil), input.TaxInputs...)
	sort.SliceStable(taxInputs, func(i, j int) bool { return taxInputs[i].ComponentCode < taxInputs[j].ComponentCode })
	for _, taxInput := range taxInputs {
		if taxInput.Currency != currency || taxInput.TaxableMinor > subtotal {
			return InvoiceRecord{}, ErrInvoiceSourceMismatch
		}
	}
	taxComponents, taxTotal, err := calculateInvoiceTaxes(taxInputs, input.TaxRules)
	if err != nil { return InvoiceRecord{}, err }
	if subtotal > math.MaxInt64-taxTotal { return InvoiceRecord{}, ErrInvoiceTaxOverflow }
	if (input.CorrectionOfID == "") != (input.CorrectionReason == "") ||
		(input.CorrectionOfID != "" && (input.CorrectionOfID == input.ID || input.CorrectionOfID != strings.TrimSpace(input.CorrectionOfID))) ||
		(len(input.CorrectionReason) > 500 || (input.CorrectionReason != "" && input.CorrectionReason != strings.TrimSpace(input.CorrectionReason))) ||
		(input.FinancialCorrection && (input.CorrectionOfID == "" || len(input.CompensatingJournalIDs) == 0)) {
		return InvoiceRecord{}, ErrInvoiceInvalid
	}
	compensations, err := uniqueInvoiceReferences(input.CompensatingJournalIDs, input.FinancialCorrection)
	if err != nil { return InvoiceRecord{}, err }
	record := InvoiceRecord{
		ID: input.ID, IdempotencyKey: input.IdempotencyKey, Version: input.Version, Status: "snapshot",
		IssuedAt: input.IssuedAt.UTC(), IssuerReference: input.IssuerReference,
		TenantID: input.Source.TenantID, CustomerReference: input.Source.CustomerReference,
		SourceOrderID: input.Source.OrderID, PriceVersionID: input.Source.PriceVersionID,
		Currency: currency, Lines: lines, SubtotalMinor: subtotal,
		TaxComponents: taxComponents, TaxTotalMinor: taxTotal, TotalMinor: subtotal + taxTotal,
		LedgerSemantics: input.Source.LedgerSemantics, LedgerJournalIDs: journals,
		PaymentOperationIDs: payments, CorrectionOfID: input.CorrectionOfID,
		CorrectionReason: input.CorrectionReason, FinancialCorrection: input.FinancialCorrection,
		CompensatingJournalIDs: compensations,
	}
	record.RequestHash, err = invoiceRequestHash(record)
	if err != nil { return InvoiceRecord{}, fmt.Errorf("hash invoice snapshot: %w", err) }
	return record, nil
}

// CheckInvoiceIdempotency accepts an exact replay and rejects reuse of a key
// for different immutable invoice content.
func CheckInvoiceIdempotency(existing, candidate InvoiceRecord) error {
	if existing.IdempotencyKey != candidate.IdempotencyKey || existing.RequestHash != candidate.RequestHash {
		return ErrInvoiceIdempotencyConflict
	}
	return nil
}
