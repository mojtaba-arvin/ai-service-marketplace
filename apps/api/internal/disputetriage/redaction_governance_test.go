package disputetriage

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestRedactionFailureNeverInvokesModelAndProvenanceHashesRedactedInput(t *testing.T) {
	model := &fakeModel{output:ModelOutput{JSON:validOutput(), Name:"offline", Version:"fixture-v1"}}
	svc := Service{Evidence:fakeEvidence{items:[]Evidence{{ID:"order-policy-01", Source:"policy", Text:"safe policy"}}}, Redactor:fakeRedactor{}, Model:model, Timeout:time.Second}
	req := Request{Market:"OM", Locale:"en", DisputeType:"service-quality", Reason:"Call +96890000000"}
	got, err := svc.Triage(context.Background(), req)
	if err != nil { t.Fatal(err) }
	redacted := strings.ReplaceAll(req.Reason, "+96890000000", "[PHONE]")
	fingerprint := sha256.Sum256([]byte(req.Market+"\n"+req.Locale+"\n"+req.DisputeType+"\n"+redacted))
	if got.Provenance.InputSHA256 != hex.EncodeToString(fingerprint[:]) { t.Fatalf("provenance hash does not match redacted input: %s", got.Provenance.InputSHA256) }
	if strings.Contains(model.input.Reason, "+96890000000") { t.Fatal("unredacted input reached model") }

	model.input = ModelInput{}
	svc.Redactor = fakeRedactor{err:errors.New("redaction unavailable")}
	if _, err := svc.Triage(context.Background(), req); err == nil { t.Fatal("redaction failure must fail closed") }
	if model.input.Reason != "" { t.Fatal("model invoked after redaction failure") }
}
