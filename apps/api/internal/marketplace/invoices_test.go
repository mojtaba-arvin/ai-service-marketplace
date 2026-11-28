package marketplace

import (
	"errors"
	"math"
	"math/big"
	"testing"
	"time"
)

func invoiceTestRule() InvoiceTaxRule {
	return InvoiceTaxRule{
		ID: "tax-rule-1", Version: 1, MarketID: "MKT-A",
		JurisdictionCode: "JUR-A", TaxCategory: "category-a",
		ComponentCode: "component-a", Currency: "USD",
		RateNumerator: 1, RateDenominator: 10,
		RoundingMode: InvoiceRoundHalfUp,
		EffectiveFrom: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		ApprovedBy: "operator-ref", ApprovalReference: "approval-ref-1",
		ApprovedAt: time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC),
	}
}

func invoiceTestInput() InvoiceBuildInput {
	issuedAt := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	return InvoiceBuildInput{
		ID: "invoice-1", IdempotencyKey: "issue-order-1", Version: 1,
		IssuedAt: issuedAt, IssuerReference: "issuer-ref",
		Source: InvoiceSourceSnapshot{
			TenantID: "tenant-1", CustomerReference: "customer-ref",
			OrderID: "order-1", PriceVersionID: "price-v7",
			OrderAmountMinor: 2000, OrderCurrency: "USD",
			PriceAmountMinor: 2000, PriceCurrency: "USD",
			LedgerBillableAmountMinor: 2000, LedgerCurrency: "USD",
			LedgerSemantics: "customer_billable/v1",
			LedgerJournalIDs: []string{"journal-1"},
			PaymentOperationIDs: []string{"payment-1"},
			Lines: []InvoiceLineInput{
				{SourceLineID: "line-b", Position: 20, Description: "source description B", Quantity: 1, UnitMinor: 1200, Currency: "USD"},
				{SourceLineID: "line-a", Position: 40, Description: "source description A", Quantity: 2, UnitMinor: 400, Currency: "USD"},
			},
		},
		TaxInputs: []InvoiceTaxInput{{
			MarketID: "MKT-A", JurisdictionCode: "JUR-A", TaxCategory: "category-a",
			ComponentCode: "component-a", Currency: "USD", TaxableMinor: 2000,
			CalculatedAt: issuedAt,
		}},
		TaxRules: []InvoiceTaxRule{invoiceTestRule()},
	}
}

func TestBuildInvoiceRecordSnapshotsSourceAndTaxProvenance(t *testing.T) {
	input := invoiceTestInput()
	record, err := BuildInvoiceRecord(input)
	if err != nil { t.Fatal(err) }
	if record.SubtotalMinor != 2000 || record.TaxTotalMinor != 200 || record.TotalMinor != 2200 {
		t.Fatalf("unexpected invoice totals: %#v", record)
	}
	if len(record.Lines) != 2 || record.Lines[0].SourceLineID != "line-b" || record.Lines[1].SourceLineID != "line-a" {
		t.Fatalf("source line order was not preserved: %#v", record.Lines)
	}
	if record.PriceVersionID != "price-v7" || record.TaxComponents[0].RuleID != "tax-rule-1" ||
		record.TaxComponents[0].RuleVersion != 1 || record.TaxComponents[0].ApprovalReference != "approval-ref-1" ||
		record.TaxComponents[0].CalculatedAt != input.IssuedAt {
		t.Fatalf("source or tax provenance missing: %#v", record)
	}
	if record.Currency != "USD" || record.LedgerJournalIDs[0] != "journal-1" || record.PaymentOperationIDs[0] != "payment-1" {
		t.Fatalf("currency or ledger references missing: %#v", record)
	}
}

func TestInvoiceTaxRoundingModes(t *testing.T) {
	tests := []struct{
		mode InvoiceRoundingMode
		numerator int64
		denominator int64
		want int64
	}{
		{InvoiceRoundHalfUp, 5, 2, 3},
		{InvoiceRoundHalfEven, 5, 2, 2},
		{InvoiceRoundHalfEven, 7, 2, 4},
		{InvoiceRoundDown, 5, 2, 2},
		{InvoiceRoundUp, 5, 2, 3},
	}
	for _, test := range tests {
		t.Run(string(test.mode)+string(rune('0'+test.want)), func(t *testing.T) {
			got, err := roundInvoiceTax(bigInt(test.numerator), bigInt(test.denominator), test.mode)
			if err != nil { t.Fatal(err) }
			if got.Int64() != test.want { t.Fatalf("got %d, want %d", got.Int64(), test.want) }
		})
	}
}

func bigInt(value int64) *big.Int { return big.NewInt(value) }

func TestInvoiceTaxRequiresExactMarketCurrencyAndEffectiveRule(t *testing.T) {
	input := invoiceTestInput()
	input.TaxInputs[0].MarketID = "MKT-B"
	if _, err := BuildInvoiceRecord(input); !errors.Is(err, ErrInvoiceTaxConfigurationMissing) {
		t.Fatalf("expected missing exact-market rule, got %v", err)
	}

	input = invoiceTestInput()
	input.TaxInputs[0].Currency = "EUR"
	if _, err := BuildInvoiceRecord(input); !errors.Is(err, ErrInvoiceSourceMismatch) {
		t.Fatalf("expected currency isolation failure, got %v", err)
	}

	input = invoiceTestInput()
	end := input.IssuedAt
	rule := invoiceTestRule()
	rule.EffectiveUntil = &end
	input.TaxRules = []InvoiceTaxRule{rule}
	if _, err := BuildInvoiceRecord(input); !errors.Is(err, ErrInvoiceTaxConfigurationMissing) {
		t.Fatalf("expected exclusive effective-until boundary, got %v", err)
	}
}

func TestInvoiceTaxRejectsMissingAmbiguousAndUnapprovedRules(t *testing.T) {
	input := invoiceTestInput()
	input.TaxRules = nil
	if _, err := BuildInvoiceRecord(input); !errors.Is(err, ErrInvoiceTaxConfigurationMissing) {
		t.Fatalf("expected missing tax config, got %v", err)
	}

	input = invoiceTestInput()
	input.TaxRules = append(input.TaxRules, invoiceTestRule())
	if _, err := BuildInvoiceRecord(input); !errors.Is(err, ErrInvoiceTaxConfigurationAmbiguous) {
		t.Fatalf("expected ambiguous tax config, got %v", err)
	}

	input = invoiceTestInput()
	input.TaxRules[0].ApprovalReference = ""
	if _, err := BuildInvoiceRecord(input); !errors.Is(err, ErrInvoiceTaxConfigurationInvalid) {
		t.Fatalf("expected missing approval provenance rejection, got %v", err)
	}

	input = invoiceTestInput()
	input.TaxRules[0].ApprovedAt = input.IssuedAt.Add(time.Hour)
	if _, err := BuildInvoiceRecord(input); !errors.Is(err, ErrInvoiceTaxConfigurationInvalid) {
		t.Fatalf("expected future approval rejection, got %v", err)
	}
}

func TestInvoiceRejectsOrderPriceLedgerAndCurrencyMismatch(t *testing.T) {
	tests := []struct{
		name string
		mutate func(*InvoiceBuildInput)
	}{
		{"price version amount", func(i *InvoiceBuildInput) { i.Source.PriceAmountMinor++ }},
		{"ledger amount", func(i *InvoiceBuildInput) { i.Source.LedgerBillableAmountMinor++ }},
		{"ledger currency", func(i *InvoiceBuildInput) { i.Source.LedgerCurrency = "EUR" }},
		{"missing journal reference", func(i *InvoiceBuildInput) { i.Source.LedgerJournalIDs = nil }},
		{"line total", func(i *InvoiceBuildInput) { i.Source.Lines[0].UnitMinor++ }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			input := invoiceTestInput()
			test.mutate(&input)
			if _, err := BuildInvoiceRecord(input); err == nil {
				t.Fatal("expected source reconciliation failure")
			}
		})
	}
}

func TestInvoiceRejectsIntegerOverflow(t *testing.T) {
	input := invoiceTestInput()
	input.Source.Lines = []InvoiceLineInput{{
		SourceLineID: "huge", Position: 1, Description: "source",
		Quantity: math.MaxInt64, UnitMinor: 2, Currency: "USD",
	}}
	input.Source.OrderAmountMinor = math.MaxInt64
	input.Source.PriceAmountMinor = math.MaxInt64
	input.Source.LedgerBillableAmountMinor = math.MaxInt64
	if _, err := BuildInvoiceRecord(input); !errors.Is(err, ErrInvoiceTaxOverflow) {
		t.Fatalf("expected line multiplication overflow, got %v", err)
	}

	input = invoiceTestInput()
	input.TaxInputs[0].TaxableMinor = math.MaxInt64
	input.TaxRules[0].RateNumerator = math.MaxInt64
	input.TaxRules[0].RateDenominator = 1
	input.Source.OrderAmountMinor = math.MaxInt64
	input.Source.PriceAmountMinor = math.MaxInt64
	input.Source.LedgerBillableAmountMinor = math.MaxInt64
	input.Source.Lines = []InvoiceLineInput{{SourceLineID:"max", Position:1, Description:"source", Quantity:1, UnitMinor:math.MaxInt64, Currency:"USD"}}
	if _, err := BuildInvoiceRecord(input); !errors.Is(err, ErrInvoiceTaxOverflow) {
		t.Fatalf("expected tax overflow, got %v", err)
	}
}

func TestInvoiceIdempotencyAndCorrectionAreAppendOnlySnapshots(t *testing.T) {
	originalInput := invoiceTestInput()
	original, err := BuildInvoiceRecord(originalInput)
	if err != nil { t.Fatal(err) }
	replay, err := BuildInvoiceRecord(originalInput)
	if err != nil { t.Fatal(err) }
	if err := CheckInvoiceIdempotency(original, replay); err != nil { t.Fatalf("exact replay rejected: %v", err) }

	// The same client request must replay the original snapshot after tax rules
	// change; the persistence boundary returns original rather than re-issuing.
	configChanged := invoiceTestInput()
	configChanged.ID = "candidate-after-config-change"
	configChanged.Version = 9
	configChanged.IssuedAt = configChanged.IssuedAt.Add(24 * time.Hour)
	configChanged.TaxInputs[0].CalculatedAt = configChanged.TaxInputs[0].CalculatedAt.Add(24 * time.Hour)
	configChanged.TaxRules[0].ID = "tax-rule-2"
	configChanged.TaxRules[0].Version = 2
	configChanged.TaxRules[0].RateNumerator = 2
	configChanged.TaxRules[0].ApprovedAt = configChanged.TaxRules[0].ApprovedAt.Add(24 * time.Hour)
	afterConfigChange, err := BuildInvoiceRecord(configChanged)
	if err != nil { t.Fatal(err) }
	if afterConfigChange.RequestHash != original.RequestHash || afterConfigChange.TotalMinor == original.TotalMinor {
		t.Fatal("request hash must be stable while the resolved tax snapshot changes")
	}
	if err := CheckInvoiceIdempotency(original, afterConfigChange); err != nil {
		t.Fatalf("configuration change must not invalidate replay of the original request: %v", err)
	}

	changed := invoiceTestInput()
	changed.Source.Lines[0].Description = "changed source description"
	candidate, err := BuildInvoiceRecord(changed)
	if err != nil { t.Fatal(err) }
	if err := CheckInvoiceIdempotency(original, candidate); !errors.Is(err, ErrInvoiceIdempotencyConflict) {
		t.Fatalf("expected changed-payload conflict, got %v", err)
	}

	correction := invoiceTestInput()
	correction.ID = "invoice-2"
	correction.IdempotencyKey = "correct-order-1"
	correction.Version = 2
	correction.CorrectionOfID = original.ID
	correction.CorrectionReason = "source order cancelled"
	correction.FinancialCorrection = true
	correction.CompensatingJournalIDs = []string{"journal-compensation-1"}
	corrected, err := BuildInvoiceRecord(correction)
	if err != nil { t.Fatal(err) }
	if corrected.ID == original.ID || corrected.CorrectionOfID != original.ID ||
		!corrected.FinancialCorrection || corrected.CompensatingJournalIDs[0] != "journal-compensation-1" {
		t.Fatalf("correction is not a new linked record: %#v", corrected)
	}
	if original.Status != "snapshot" || original.ID != "invoice-1" || original.TotalMinor != 2200 {
		t.Fatalf("original invoice snapshot was mutated: %#v", original)
	}
}


func TestInvoiceCancellationCreatesLinkedRecordWithoutVoidingHistory(t *testing.T) {
	original, err := BuildInvoiceRecord(invoiceTestInput())
	if err != nil { t.Fatal(err) }

	cancellation := invoiceTestInput()
	cancellation.ID = "invoice-cancellation-1"
	cancellation.IdempotencyKey = "cancel-order-1"
	cancellation.Version = 2
	cancellation.CorrectionOfID = original.ID
	cancellation.CorrectionReason = "source order cancelled"
	cancelled, err := BuildInvoiceRecord(cancellation)
	if err != nil { t.Fatal(err) }
	if cancelled.ID == original.ID || cancelled.CorrectionOfID != original.ID ||
		cancelled.CorrectionReason != "source order cancelled" {
		t.Fatalf("cancellation must be represented by a new linked record: %#v", cancelled)
	}
	if original.Status != "snapshot" || original.TotalMinor != 2200 || original.CorrectionOfID != "" {
		t.Fatalf("cancellation mutated the original invoice snapshot: %#v", original)
	}
}

func TestInvoiceRejectsFinancialCorrectionWithoutCompensatingJournal(t *testing.T) {
	input := invoiceTestInput()
	input.CorrectionOfID = "invoice-original"
	input.CorrectionReason = "financial correction"
	input.FinancialCorrection = true
	if _, err := BuildInvoiceRecord(input); !errors.Is(err, ErrInvoiceInvalid) {
		t.Fatalf("expected compensating journal requirement, got %v", err)
	}
}
