package aiintake

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"testing"
	"time"
)

func TestRedactionFailureNeverInvokesModelAndProvenanceHashesRedactedText(t *testing.T) {
	s, model := serviceFor(`{"category_id":"ac-repair","summary":"AC issue","required_details":["Unit model"],"confidence":0.8}`)
	s.Redactor = fakeRedactor{text:"redacted input"}
	_, err := s.Suggest(context.Background(), Request{Market:"OM", Locale:"en", Text:"raw private input"})
	if err != nil { t.Fatal(err) }
	sum := sha256.Sum256([]byte("redacted input"))
	if model.input.Text != "redacted input" { t.Fatalf("model received unexpected input: %q", model.input.Text) }
	// The service owns the provenance hash; compare it with the exact redacted payload.
	got, err := s.Suggest(context.Background(), Request{Market:"OM", Locale:"en", Text:"raw private input"})
	if err != nil { t.Fatal(err) }
	if got.Provenance.InputSHA256 != hex.EncodeToString(sum[:]) { t.Fatalf("hash does not describe redacted input: %s", got.Provenance.InputSHA256) }

	s.Redactor = fakeRedactor{err:errors.New("redaction unavailable")}
	model.input = ModelInput{}
	if _, err := s.Suggest(context.Background(), Request{Market:"OM", Locale:"en", Text:"raw private input"}); err == nil { t.Fatal("redaction failure must fail closed") }
	if model.input.Text != "" { t.Fatal("model invoked after redaction failure") }
	_ = time.Second
}
