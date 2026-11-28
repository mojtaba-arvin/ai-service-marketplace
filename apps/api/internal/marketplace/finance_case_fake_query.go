package marketplace

import (
	"context"
	"strings"
)

// QueryFinanceCase implements the read-only FIN-003 fake-provider recovery
// contract. It only inspects an already recorded fake result; it never submits
// or retries a provider operation.
func (p *FakePaymentProvider) QueryFinanceCase(ctx context.Context, providerCode, caseType, key string, amount int64, currency string) (FinanceCaseProviderResult, error) {
	if err := ctx.Err(); err != nil { return FinanceCaseProviderResult{}, err }
	providerCode = strings.ToLower(strings.TrimSpace(providerCode))
	caseType = strings.ToLower(strings.TrimSpace(caseType))
	key = strings.TrimSpace(key)
	currency = strings.ToUpper(strings.TrimSpace(currency))
	if providerCode == "" || key == "" || amount <= 0 || ValidateAmount(amount,currency) != nil {
		return FinanceCaseProviderResult{}, ErrFinanceCaseInput
	}
	p.mu.Lock()
	result, ok := p.results[caseType+":"+key]
	p.mu.Unlock()
	outcome := "unknown"
	if ok {
		switch result.Status {
		case "succeeded":
			outcome = "confirmed"
		case "failed":
			outcome = "failed"
		}
	}
	return FinanceCaseProviderResult{
		ProviderCode: providerCode,
		ProviderIdempotencyKey: key,
		Outcome: outcome,
		ProviderReference: result.ExternalReference,
		AmountMinor: amount,
		Currency: currency,
		FailureCode: result.FailureCode,
	}, nil
}
