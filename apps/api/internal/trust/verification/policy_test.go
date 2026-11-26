package verification

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/mojtaba-arvin/ai-service-marketplace/apps/api/internal/marketconfig"
)

type fakeAdapter struct {
	result ProviderResult
	request Request
	err error
}

func (f *fakeAdapter) Check(_ context.Context, request Request) (ProviderResult, error) {
	f.request = request
	return f.result, f.err
}

func testSetup(t *testing.T, outcome Outcome) (Service, Policy, Requirement, *fakeAdapter, time.Time) {
	t.Helper()
	registry, err := marketconfig.New([]marketconfig.Config{{
		MarketID: "OM", Locale: "en-OM", Currency: "OMR",
		TimeZone: "Asia/Muscat", PolicyVersion: "om-trust-v1",
	}})
	if err != nil { t.Fatal(err) }
	matrix, err := marketconfig.NewMatrix(registry, []marketconfig.AdapterConfig{{
		MarketID: "OM", Locale: "en-OM", PolicyVersion: "om-trust-v1",
		Adapter: "screening", Capabilities: []string{"identity_check"},
	}})
	if err != nil { t.Fatal(err) }
	requirement := Requirement{
		CheckID: "identity_check", AdapterID: "screening",
		Capability: "identity_check", ConsentMode: ConsentRequired,
		RecheckAfter: 30 * 24 * time.Hour,
	}
	policy := Policy{
		MarketID: "OM", Locale: "en-OM", PolicyVersion: "om-trust-v1",
		ServiceRisk: "standard", Requirements: []Requirement{requirement},
	}
	now := time.Date(2026, 11, 27, 0, 0, 0, 0, time.UTC)
	adapter := &fakeAdapter{result: ProviderResult{
		Outcome: outcome, EvidenceRef: "private://evidence/check-123",
		ProviderReference: "provider-check-123", CheckedAt: now.Add(-time.Hour),
		ExpiresAt: now.Add(30 * 24 * time.Hour),
	}}
	return Service{Registry: registry, Matrix: matrix, Adapters: map[string]Adapter{"screening": adapter}},
		policy, requirement, adapter, now
}

func validConsent(now time.Time) *Consent {
	return &Consent{
		SubjectRef: "subject:123", Purpose: "identity_check",
		EvidenceRef: "private://consent/record-123", PolicyVersion: "om-trust-v1",
		GrantedAt: now.Add(-2 * time.Hour),
	}
}

func TestRunCheckUsesExactMarketCapabilityAndMinimizedRequest(t *testing.T) {
	service, policy, requirement, adapter, now := testSetup(t, OutcomeClear)
	credential, err := service.RunCheck(context.Background(), policy, requirement, "subject:123", "idempotency-123", validConsent(now), now)
	if err != nil { t.Fatal(err) }
	if credential.Status != StatusVerified { t.Fatalf("status = %q", credential.Status) }
	if adapter.request.SubjectRef != "subject:123" || adapter.request.ConsentEvidenceRef == "" {
		t.Fatalf("provider request did not carry opaque subject and consent references: %+v", adapter.request)
	}
	if adapter.request.IdempotencyKey != "idempotency-123" || adapter.request.PolicyVersion != policy.PolicyVersion {
		t.Fatalf("provider request lost idempotency or policy provenance: %+v", adapter.request)
	}
	if credential.ReviewDueAt.After(credential.ExpiresAt) {
		t.Fatal("review due time must not exceed provider evidence expiry")
	}
}

func TestRunCheckRequiresExplicitConsentAndConfiguredAdapter(t *testing.T) {
	service, policy, requirement, _, now := testSetup(t, OutcomeClear)
	if _, err := service.RunCheck(context.Background(), policy, requirement, "subject:123", "key-123", nil, now); !errors.Is(err, ErrConsentRequired) {
		t.Fatalf("missing consent error = %v", err)
	}
	delete(service.Adapters, requirement.AdapterID)
	if _, err := service.RunCheck(context.Background(), policy, requirement, "subject:123", "key-123", validConsent(now), now); !errors.Is(err, ErrProviderUnavailable) {
		t.Fatalf("missing adapter error = %v", err)
	}
}

func TestRunCheckRejectsUnknownMarketCapabilityAndInvalidProviderEvidence(t *testing.T) {
	service, policy, requirement, adapter, now := testSetup(t, OutcomeClear)
	policy.Locale = "fr-OM"
	if _, err := service.RunCheck(context.Background(), policy, requirement, "subject:123", "key-123", validConsent(now), now); !errors.Is(err, ErrInvalidPolicy) {
		t.Fatalf("unknown locale error = %v", err)
	}
	policy.Locale = "en-OM"
	adapter.result.EvidenceRef = "https://public.example/document"
	if _, err := service.RunCheck(context.Background(), policy, requirement, "subject:123", "key-123", validConsent(now), now); !errors.Is(err, ErrInvalidProviderResult) {
		t.Fatalf("public evidence reference error = %v", err)
	}
}

func TestAdverseResultRequiresHumanReviewAndBlocksDispatch(t *testing.T) {
	service, policy, requirement, _, now := testSetup(t, OutcomeAdverse)
	credential, err := service.RunCheck(context.Background(), policy, requirement, "subject:123", "key-123", validConsent(now), now)
	if err != nil { t.Fatal(err) }
	if credential.Status != StatusReviewRequired { t.Fatalf("status = %q", credential.Status) }
	if err := EvaluateDispatch(policy, []Credential{credential}, now); !errors.Is(err, ErrCredentialNotDispatchable) {
		t.Fatalf("adverse result dispatch error = %v", err)
	}
	if _, err := Review(credential, "operator:1", "Reviewed evidence", ReviewApprove, false, now); !errors.Is(err, ErrReviewUnauthorized) {
		t.Fatalf("unauthorized review error = %v", err)
	}
	approved, err := Review(credential, "operator:1", "Reviewed evidence", ReviewApprove, true, now)
	if err != nil { t.Fatal(err) }
	if approved.Status != StatusVerified || len(approved.Events) != 2 || approved.Events[1].ActorRef != "operator:1" {
		t.Fatalf("review decision was not audited: %+v", approved)
	}
	if err := EvaluateDispatch(policy, []Credential{approved}, now); err != nil {
		t.Fatalf("approved credential should be dispatchable: %v", err)
	}
}

func TestDispatchFailsClosedForFailedRevokedExpiredAndReviewDueCredentials(t *testing.T) {
	service, policy, requirement, _, now := testSetup(t, OutcomeClear)
	credential, err := service.RunCheck(context.Background(), policy, requirement, "subject:123", "key-123", validConsent(now), now)
	if err != nil { t.Fatal(err) }
	if err := EvaluateDispatch(policy, []Credential{credential}, now); err != nil { t.Fatal(err) }

	failed := credential
	failed.Status = StatusFailed
	if err := EvaluateDispatch(policy, []Credential{failed}, now); !errors.Is(err, ErrCredentialNotDispatchable) {
		t.Fatalf("failed credential error = %v", err)
	}
	revoked, err := Review(credential, "operator:1", "Credential revoked", ReviewRevoke, true, now)
	if err != nil { t.Fatal(err) }
	if err := EvaluateDispatch(policy, []Credential{revoked}, now); !errors.Is(err, ErrCredentialNotDispatchable) {
		t.Fatalf("revoked credential error = %v", err)
	}
	if err := EvaluateDispatch(policy, []Credential{credential}, credential.ExpiresAt); !errors.Is(err, ErrCredentialExpired) {
		t.Fatalf("expired credential error = %v", err)
	}
	if err := EvaluateDispatch(policy, []Credential{credential}, credential.ReviewDueAt); !errors.Is(err, ErrCredentialReviewDue) {
		t.Fatalf("review-due credential error = %v", err)
	}
}

func TestPolicyRejectsMissingConsentPolicyAndUnconfiguredCapability(t *testing.T) {
	service, policy, requirement, _, _ := testSetup(t, OutcomeClear)
	requirement.ConsentMode = ""
	policy.Requirements = []Requirement{requirement}
	if err := policy.Validate(service.Registry, service.Matrix); !errors.Is(err, ErrInvalidPolicy) {
		t.Fatalf("implicit consent policy error = %v", err)
	}
	requirement.ConsentMode = ConsentRequired
	requirement.Capability = "criminal_records"
	policy.Requirements = []Requirement{requirement}
	if err := policy.Validate(service.Registry, service.Matrix); !errors.Is(err, ErrInvalidPolicy) {
		t.Fatalf("unconfigured capability error = %v", err)
	}
}
