package marketplace

import (
	"context"
	"encoding/json"
	"testing"
)

func TestFinanceCaseWebhookVerifierAuthenticatesAndValidates(t *testing.T) {
	verifier := HMACFinanceCaseWebhookVerifier{Secret: []byte("offline-fake-webhook-secret-at-least-32-bytes")}
	event := FinanceCaseWebhook{
		EventID: "evt-fin-1", ProviderCode: "fake", ProviderIdempotencyKey: "case-key-1",
		Outcome: "confirmed", ProviderReference: "fake-ref-1", AmountMinor: 1250, Currency: "OMR",
	}
	payload, err := json.Marshal(event)
	if err != nil { t.Fatal(err) }
	signature := verifier.Sign(payload)
	got, err := verifier.VerifyFinanceCaseWebhook(context.Background(), payload, signature)
	if err != nil || got.EventID != event.EventID || got.AmountMinor != event.AmountMinor {
		t.Fatalf("valid signed callback rejected: %#v %v", got, err)
	}
	if _, err = verifier.VerifyFinanceCaseWebhook(context.Background(), payload, "00"); err == nil {
		t.Fatal("invalid signature must be rejected")
	}
	if _, err = (HMACFinanceCaseWebhookVerifier{Secret: []byte("short")}).VerifyFinanceCaseWebhook(
		context.Background(), payload, signature); err == nil {
		t.Fatal("short/missing provider secret must fail closed")
	}
	event.Outcome = "succeeded_http_200"
	payload, _ = json.Marshal(event)
	if _, err = verifier.VerifyFinanceCaseWebhook(context.Background(), payload, verifier.Sign(payload)); err == nil {
		t.Fatal("HTTP success or unknown provider outcomes must not be treated as confirmation")
	}
	event.Outcome = "confirmed"
	event.ProviderReference = ""
	payload, _ = json.Marshal(event)
	if _, err = verifier.VerifyFinanceCaseWebhook(context.Background(), payload, verifier.Sign(payload)); err == nil {
		t.Fatal("confirmation without provider reference must fail closed")
	}
}
