package marketplace

import (
	"context"
	"encoding/json"
	"testing"
)

func TestPaymentProviderFinanceCaseWebhookVerifierUsesFIN003Contract(t *testing.T) {
	provider := NewFakePaymentProvider(FakeProviderSuccess, []byte("offline-fake-webhook-secret-at-least-32-bytes"))
	verifier := PaymentProviderFinanceCaseWebhookVerifier{Provider: provider, ProviderCode: "fake"}
	event := PaymentWebhook{
		EventID: "evt-fin-1", OperationKey: "case-key-1", Status: "succeeded",
		ExternalReference: "fake-ref-1", AmountMinor: 1250, Currency: "OMR",
	}
	payload, err := json.Marshal(event)
	if err != nil { t.Fatal(err) }
	got, err := verifier.VerifyFinanceCaseWebhook(context.Background(), payload, provider.SignWebhook(payload))
	if err != nil || got.EventID != event.EventID || got.ProviderIdempotencyKey != event.OperationKey ||
		got.Outcome != "confirmed" || got.AmountMinor != event.AmountMinor {
		t.Fatalf("valid FIN-003 callback rejected: %#v %v", got, err)
	}
	if _, err = verifier.VerifyFinanceCaseWebhook(context.Background(), payload, "00"); err == nil {
		t.Fatal("invalid provider signature must be rejected")
	}
	event.Status = "succeeded_http_200"
	payload, _ = json.Marshal(event)
	if _, err = verifier.VerifyFinanceCaseWebhook(context.Background(), payload, provider.SignWebhook(payload)); err == nil {
		t.Fatal("HTTP success or unknown provider outcomes must not be treated as confirmation")
	}
	event.Status = "succeeded"
	event.AmountMinor = 0
	payload, _ = json.Marshal(event)
	if _, err = verifier.VerifyFinanceCaseWebhook(context.Background(), payload, provider.SignWebhook(payload)); err == nil {
		t.Fatal("confirmation without explicit amount/currency must fail closed")
	}
	event.AmountMinor = 1250
	event.Status = "failed"
	event.FailureCode = "fake_declined"
	event.ExternalReference = ""
	payload, _ = json.Marshal(event)
	got, err = verifier.VerifyFinanceCaseWebhook(context.Background(), payload, provider.SignWebhook(payload))
	if err != nil || got.Outcome != "failed" || got.FailureCode != event.FailureCode {
		t.Fatalf("failed provider result must remain distinct: %#v %v", got, err)
	}
}
