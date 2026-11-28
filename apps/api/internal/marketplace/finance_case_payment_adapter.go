package marketplace

import (
	"context"
	"errors"
	"strings"
)

// PaymentProviderFinanceCaseWebhookVerifier adapts the existing FIN-003
// PaymentProvider authentication contract to finance case outcomes.
type PaymentProviderFinanceCaseWebhookVerifier struct {
	Provider PaymentProvider
	ProviderCode string
}

func (v PaymentProviderFinanceCaseWebhookVerifier) VerifyFinanceCaseWebhook(ctx context.Context, payload []byte, signature string) (FinanceCaseWebhook, error) {
	if v.Provider == nil || strings.TrimSpace(v.ProviderCode) == "" {
		return FinanceCaseWebhook{}, ErrFinanceCaseInput
	}
	event, err := v.Provider.VerifyWebhook(ctx, payload, signature)
	if err != nil {
		return FinanceCaseWebhook{}, err
	}
	outcome := ""
	switch event.Status {
	case "succeeded":
		outcome = "confirmed"
	case "failed":
		outcome = "failed"
	default:
		return FinanceCaseWebhook{}, errors.New("unsupported payment provider webhook status")
	}
	result := FinanceCaseWebhook{
		EventID: event.EventID,
		ProviderCode: strings.ToLower(strings.TrimSpace(v.ProviderCode)),
		ProviderIdempotencyKey: event.OperationKey,
		Outcome: outcome,
		ProviderReference: event.ExternalReference,
		AmountMinor: event.AmountMinor,
		Currency: event.Currency,
		FailureCode: event.FailureCode,
	}
	if result.AmountMinor <= 0 || ValidateAmount(result.AmountMinor, strings.ToUpper(strings.TrimSpace(result.Currency))) != nil {
		return FinanceCaseWebhook{}, ErrFinanceCaseInput
	}
	if outcome == "confirmed" && strings.TrimSpace(result.ProviderReference) == "" {
		return FinanceCaseWebhook{}, ErrFinanceCaseInput
	}
	if outcome == "failed" && strings.TrimSpace(result.FailureCode) == "" {
		return FinanceCaseWebhook{}, ErrFinanceCaseInput
	}
	return result, nil
}
