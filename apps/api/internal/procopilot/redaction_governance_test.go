package procopilot

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
	svc := Service{Knowledge:fakeKnowledge{evidence:[]Evidence{{ID:"manual-ac-01", Source:"manual", Text:"safe evidence"}}}, Model:model, Redactor:fakeRedactor{}, Timeout:time.Second}
	req := Request{Market:"OM", Locale:"en", ServiceCode:"repair", Description:"Call 555-0100", EquipmentContext:"Unit"}
	got, err := svc.Suggest(context.Background(), req)
	if err != nil { t.Fatal(err) }
	redacted := strings.ReplaceAll(req.Description, "555-0100", "[REDACTED]")
	fingerprintInput, err := json.Marshal(ModelInput{Market:req.Market, Locale:req.Locale, ServiceCode:req.ServiceCode, Description:redacted, EquipmentContext:req.EquipmentContext, Evidence:[]Evidence{{ID:"manual-ac-01", Source:"manual", Text:"safe evidence"}}})
	if err != nil { t.Fatal(err) }
	fingerprint := sha256.Sum256(fingerprintInput)
	if got.Provenance.InputSHA256 != hex.EncodeToString(fingerprint[:]) { t.Fatalf("provenance hash does not match redacted input: %s", got.Provenance.InputSHA256) }
	if strings.Contains(model.input.Description, "555-0100") { t.Fatal("unredacted input reached model") }

	model.input = ModelInput{}
	svc.Redactor = fakeRedactor{err:errors.New("redaction unavailable")}
	if _, err := svc.Suggest(context.Background(), req); err == nil { t.Fatal("redaction failure must fail closed") }
	if model.input.Description != "" { t.Fatal("model invoked after redaction failure") }
}
