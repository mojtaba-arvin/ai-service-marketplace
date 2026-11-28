// Package verification contains provider-neutral credential review rules.
// Persistence, HTTP registration, and operator authorization remain at the owning
// application boundary; this package never stores raw identity documents.
package verification

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/mojtaba-arvin/ai-service-marketplace/apps/api/internal/marketconfig"
)

var (
	ErrInvalidPolicy = errors.New("invalid credential verification policy")
	ErrConsentRequired = errors.New("valid consent evidence is required")
	ErrProviderUnavailable = errors.New("configured verification provider is unavailable")
	ErrInvalidProviderResult = errors.New("invalid verification provider result")
	ErrCredentialNotDispatchable = errors.New("credential is not eligible for restricted dispatch")
	ErrCredentialExpired = errors.New("credential evidence has expired")
	ErrCredentialReviewDue = errors.New("credential review is due")
	ErrReviewUnauthorized = errors.New("authorized human review is required")
	ErrInvalidReview = errors.New("invalid credential review")
)

var identifierPattern = regexp.MustCompile(`^[a-z][a-z0-9._-]{0,63}$`)

type ConsentMode string

const (
	ConsentRequired ConsentMode = "required"
	ConsentNotRequired ConsentMode = "not_required"
)

type Requirement struct {
	CheckID string
	AdapterID string
	Capability string
	ConsentMode ConsentMode
	RecheckAfter time.Duration
}

type Policy struct {
	TenantRef string
	MarketID string
	Locale string
	PolicyVersion string
	ServiceRisk string
	Requirements []Requirement
}

// Validate requires an explicit, exact market policy and adapter capability
// configuration. It deliberately supplies no default providers, consent rules,
// service-risk levels, or review intervals.
func (p Policy) Validate(registry *marketconfig.Registry, matrix *marketconfig.Matrix) error {
	if registry == nil || matrix == nil || p.TenantRef == "" || strings.TrimSpace(p.TenantRef) != p.TenantRef || !identifierPattern.MatchString(p.ServiceRisk) || p.PolicyVersion == "" {
		return ErrInvalidPolicy
	}
	market, err := registry.Resolve(p.MarketID, p.Locale)
	if err != nil || market.PolicyVersion != p.PolicyVersion {
		return fmt.Errorf("%w: market, locale, or policy version is not configured exactly", ErrInvalidPolicy)
	}
	if len(p.Requirements) == 0 {
		return fmt.Errorf("%w: at least one explicit check requirement is required", ErrInvalidPolicy)
	}
	seen := make(map[string]struct{}, len(p.Requirements))
	for _, requirement := range p.Requirements {
		if !identifierPattern.MatchString(requirement.CheckID) ||
			!identifierPattern.MatchString(requirement.AdapterID) ||
			!identifierPattern.MatchString(requirement.Capability) ||
			requirement.RecheckAfter <= 0 ||
			(requirement.ConsentMode != ConsentRequired && requirement.ConsentMode != ConsentNotRequired) {
			return ErrInvalidPolicy
		}
		if _, exists := seen[requirement.CheckID]; exists {
			return fmt.Errorf("%w: duplicate check %q", ErrInvalidPolicy, requirement.CheckID)
		}
		seen[requirement.CheckID] = struct{}{}
		if err := matrix.Require(p.MarketID, p.Locale, requirement.AdapterID, requirement.Capability); err != nil {
			return fmt.Errorf("%w: %v", ErrInvalidPolicy, err)
		}
	}
	return nil
}

type Consent struct {
	SubjectRef string
	Purpose string
	EvidenceRef string
	PolicyVersion string
	GrantedAt time.Time
	RevokedAt *time.Time
}

func (c Consent) valid(subjectRef, purpose, policyVersion string, now time.Time) bool {
	return c.SubjectRef == subjectRef && subjectRef != "" &&
		c.Purpose == purpose && purpose != "" &&
		c.PolicyVersion == policyVersion &&
		c.EvidenceRef != "" && validPrivateReference(c.EvidenceRef) &&
		!c.GrantedAt.IsZero() && !c.GrantedAt.After(now) &&
		(c.RevokedAt == nil || c.RevokedAt.After(now))
}

// Request contains opaque subject and consent references only. Provider
// adapters must not receive raw identity documents or unnecessary profile data.
type Request struct {
	IdempotencyKey string
	SubjectRef string
	MarketID string
	Locale string
	PolicyVersion string
	ServiceRisk string
	CheckID string
	ConsentEvidenceRef string
}

type Outcome string

const (
	OutcomeClear Outcome = "clear"
	OutcomeAdverse Outcome = "adverse"
	OutcomeFailed Outcome = "failed"
)

type ProviderResult struct {
	Outcome Outcome
	EvidenceRef string
	ProviderReference string
	CheckedAt time.Time
	ExpiresAt time.Time
}

type Adapter interface {
	Check(context.Context, Request) (ProviderResult, error)
}

type Status string

const (
	StatusVerified Status = "verified"
	StatusReviewRequired Status = "review_required"
	StatusFailed Status = "failed"
	StatusRevoked Status = "revoked"
	StatusRecheckDue Status = "recheck_due"
	StatusExpired Status = "expired"
)

type Event struct {
	Action string
	ActorRef string
	Reason string
	At time.Time
	From Status
	To Status
	EvidenceRef string
	ProviderReference string
}

type Credential struct {
	TenantRef string
	SubjectRef string
	MarketID string
	Locale string
	PolicyVersion string
	ServiceRisk string
	CheckID string
	AdapterID string
	Status Status
	ProviderOutcome Outcome
	EvidenceRef string
	ProviderReference string
	CheckedAt time.Time
	ExpiresAt time.Time
	ReviewDueAt time.Time
	ReviewerRef string
	DecisionReason string
	Events []Event
}

type Service struct {
	Registry *marketconfig.Registry
	Matrix *marketconfig.Matrix
	Adapters map[string]Adapter
}

func (s Service) RunCheck(ctx context.Context, policy Policy, requirement Requirement, subjectRef, idempotencyKey string, consent *Consent, now time.Time) (Credential, error) {
	if err := policy.Validate(s.Registry, s.Matrix); err != nil {
		return Credential{}, err
	}
	if subjectRef == "" || strings.TrimSpace(subjectRef) != subjectRef ||
		idempotencyKey == "" || strings.TrimSpace(idempotencyKey) != idempotencyKey ||
		now.IsZero() {
		return Credential{}, ErrInvalidPolicy
	}
	configured := false
	for _, candidate := range policy.Requirements {
		if candidate == requirement {
			configured = true
			break
		}
	}
	if !configured {
		return Credential{}, ErrInvalidPolicy
	}
	if requirement.ConsentMode == ConsentRequired {
		if consent == nil || !consent.valid(subjectRef, requirement.CheckID, policy.PolicyVersion, now) {
			return Credential{}, ErrConsentRequired
		}
	}
	adapter := s.Adapters[requirement.AdapterID]
	if adapter == nil {
		return Credential{}, ErrProviderUnavailable
	}
	request := Request{
		IdempotencyKey: idempotencyKey,
		SubjectRef: subjectRef,
		MarketID: policy.MarketID,
		Locale: policy.Locale,
		PolicyVersion: policy.PolicyVersion,
		ServiceRisk: policy.ServiceRisk,
		CheckID: requirement.CheckID,
	}
	if consent != nil && requirement.ConsentMode == ConsentRequired {
		request.ConsentEvidenceRef = consent.EvidenceRef
	}
	result, err := adapter.Check(ctx, request)
	if err != nil {
		return Credential{}, err
	}
	if requirement.ConsentMode == ConsentRequired && consent.GrantedAt.After(result.CheckedAt) {
		return Credential{}, ErrConsentRequired
	}
	if !validProviderResult(result, now) {
		return Credential{}, ErrInvalidProviderResult
	}
	status := StatusVerified
	switch result.Outcome {
	case OutcomeClear:
	case OutcomeAdverse:
		status = StatusReviewRequired
	case OutcomeFailed:
		status = StatusFailed
	default:
		return Credential{}, ErrInvalidProviderResult
	}
	reviewDueAt := result.ExpiresAt
	configuredReviewAt := result.CheckedAt.Add(requirement.RecheckAfter)
	if configuredReviewAt.Before(reviewDueAt) {
		reviewDueAt = configuredReviewAt
	}
	credential := Credential{
		TenantRef: policy.TenantRef,
		SubjectRef: subjectRef,
		MarketID: policy.MarketID,
		Locale: policy.Locale,
		PolicyVersion: policy.PolicyVersion,
		ServiceRisk: policy.ServiceRisk,
		CheckID: requirement.CheckID,
		AdapterID: requirement.AdapterID,
		Status: status,
		ProviderOutcome: result.Outcome,
		EvidenceRef: result.EvidenceRef,
		ProviderReference: result.ProviderReference,
		CheckedAt: result.CheckedAt,
		ExpiresAt: result.ExpiresAt,
		ReviewDueAt: reviewDueAt,
		Events: []Event{{
			Action: "provider_result",
			At: result.CheckedAt,
			To: status,
			EvidenceRef: result.EvidenceRef,
			ProviderReference: result.ProviderReference,
		}},
	}
	return credential, nil
}

func validProviderResult(result ProviderResult, now time.Time) bool {
	return (result.Outcome == OutcomeClear || result.Outcome == OutcomeAdverse || result.Outcome == OutcomeFailed) &&
		validPrivateReference(result.EvidenceRef) &&
		result.ProviderReference != "" && strings.TrimSpace(result.ProviderReference) == result.ProviderReference &&
		!result.CheckedAt.IsZero() && !result.CheckedAt.After(now) &&
		!result.ExpiresAt.IsZero() && result.ExpiresAt.After(result.CheckedAt)
}

func validPrivateReference(value string) bool {
	return len(value) >= 12 && len(value) <= 512 &&
		strings.TrimSpace(value) == value &&
		strings.HasPrefix(value, "private://") &&
		!strings.ContainsAny(value, "?#\r\n")
}

// AdvanceCredential records the expiry or periodic recheck transition. It is
// idempotent: repeated scheduler delivery does not append duplicate events.
func AdvanceCredential(credential Credential, now time.Time) (Credential, error) {
	if now.IsZero() || now.Before(credential.CheckedAt) {
		return Credential{}, ErrInvalidReview
	}
	if credential.Status == StatusRevoked || credential.Status == StatusExpired {
		return credential, nil
	}
	nextStatus := credential.Status
	action := ""
	if !now.Before(credential.ExpiresAt) {
		nextStatus = StatusExpired
		action = "expired"
	} else if !now.Before(credential.ReviewDueAt) &&
		(credential.Status == StatusVerified || credential.Status == StatusReviewRequired || credential.Status == StatusFailed) {
		nextStatus = StatusRecheckDue
		action = "recheck_due"
	}
	if action == "" {
		return credential, nil
	}
	next := credential
	next.Events = append([]Event(nil), credential.Events...)
	next.Status = nextStatus
	next.Events = append(next.Events, Event{
		Action: action,
		At: now,
		From: credential.Status,
		To: nextStatus,
		EvidenceRef: credential.EvidenceRef,
		ProviderReference: credential.ProviderReference,
	})
	return next, nil
}

type ReviewDecision string

const (
	ReviewApprove ReviewDecision = "approve"
	ReviewReject ReviewDecision = "reject"
	ReviewRevoke ReviewDecision = "revoke"
)

// Review applies a reasoned human decision. The caller must first enforce the
// target tenant/operator permission using the application authorization boundary;
// Authorized is an assertion from that trusted boundary, never request input.
func Review(credential Credential, reviewerRef, reason string, decision ReviewDecision, authorized bool, now time.Time) (Credential, error) {
	if !authorized || reviewerRef == "" || strings.TrimSpace(reviewerRef) != reviewerRef || reviewerRef == credential.SubjectRef {
		return Credential{}, ErrReviewUnauthorized
	}
	reason = strings.TrimSpace(reason)
	if reason == "" || len(reason) > 1000 || now.IsZero() || now.Before(credential.CheckedAt) {
		return Credential{}, ErrInvalidReview
	}
	if credential.Status != StatusReviewRequired && credential.Status != StatusVerified && credential.Status != StatusFailed {
		return Credential{}, ErrInvalidReview
	}
	next := credential
	next.Events = append([]Event(nil), credential.Events...)
	from := credential.Status
	switch decision {
	case ReviewApprove:
		if (from != StatusReviewRequired && from != StatusFailed) || !now.Before(credential.ExpiresAt) || !now.Before(credential.ReviewDueAt) {
			return Credential{}, ErrInvalidReview
		}
		next.Status = StatusVerified
	case ReviewReject:
		if from != StatusReviewRequired && from != StatusFailed {
			return Credential{}, ErrInvalidReview
		}
		next.Status = StatusFailed
	case ReviewRevoke:
		next.Status = StatusRevoked
	default:
		return Credential{}, ErrInvalidReview
	}
	next.ReviewerRef = reviewerRef
	next.DecisionReason = reason
	next.Events = append(next.Events, Event{
		Action: string(decision),
		ActorRef: reviewerRef,
		Reason: reason,
		At: now,
		From: from,
		To: next.Status,
		EvidenceRef: credential.EvidenceRef,
		ProviderReference: credential.ProviderReference,
	})
	return next, nil
}

// EvaluateDispatch fails closed unless every configured check is current,
// verified, and bound to the exact market policy version and service risk.
func EvaluateDispatch(policy Policy, credentials []Credential, now time.Time) error {
	if now.IsZero() || len(policy.Requirements) == 0 {
		return ErrInvalidPolicy
	}
	byCheck := make(map[string]Credential, len(credentials))
	subjectRef := ""
	for _, credential := range credentials {
		if _, exists := byCheck[credential.CheckID]; exists {
			return ErrInvalidPolicy
		}
		if subjectRef == "" {
			subjectRef = credential.SubjectRef
		} else if credential.SubjectRef != subjectRef {
			return ErrCredentialNotDispatchable
		}
		byCheck[credential.CheckID] = credential
	}
	for _, requirement := range policy.Requirements {
		credential, exists := byCheck[requirement.CheckID]
		if !exists || credential.SubjectRef == "" || credential.TenantRef != policy.TenantRef ||
			credential.MarketID != policy.MarketID ||
			credential.Locale != policy.Locale ||
			credential.PolicyVersion != policy.PolicyVersion ||
			credential.ServiceRisk != policy.ServiceRisk ||
			credential.CheckID != requirement.CheckID ||
			credential.AdapterID != requirement.AdapterID {
			return ErrCredentialNotDispatchable
		}
		if credential.Status != StatusVerified ||
			(credential.ProviderOutcome != OutcomeClear && credential.ProviderOutcome != OutcomeAdverse && credential.ProviderOutcome != OutcomeFailed) ||
			((credential.ProviderOutcome == OutcomeAdverse || credential.ProviderOutcome == OutcomeFailed) && credential.ReviewerRef == "") {
			return ErrCredentialNotDispatchable
		}
		if !now.Before(credential.ExpiresAt) {
			return ErrCredentialExpired
		}
		if !now.Before(credential.ReviewDueAt) {
			return ErrCredentialReviewDue
		}
	}
	return nil
}
