package analytics

import (
	"errors"
	"strings"
	"time"
)

const (
	DefaultAllowedLateness = 72 * time.Hour
	EventTimePolicyVersion = "event-time-v1"
)

var ErrInvalidEventTime = errors.New("invalid event time or ingestion time")

type EventTimePolicy struct {
	MetricFamily string
	Version string
	AllowedLateness time.Duration
}

type EventTimeClassification struct {
	Late bool
	ClockSkew bool
}

// EventTimePolicyFor maps supported canonical producer families to an explicit
// lateness policy. The 72-hour value applies only to order-funnel and provider
// supply; it is not a retention, legal, contractual, quality-maturity, or
// financial-close rule.
func MetricFamilyForEventType(eventType string) string {
	switch {
	case strings.HasPrefix(eventType, "order."), strings.HasPrefix(eventType, "dispatch."):
		return "order_funnel"
	case strings.HasPrefix(eventType, "provider."):
		return "provider_supply"
	case strings.HasPrefix(eventType, "review."), strings.HasPrefix(eventType, "trust."):
		return "service_quality"
	case strings.HasPrefix(eventType, "finance."), strings.HasPrefix(eventType, "ledger."):
		return "finance"
	default:
		return ""
	}
}

// EventTimePolicyFor returns a policy only for families whose metric owners
// have defined a bounded-lateness contract. Quality and finance fail closed.
func EventTimePolicyFor(eventType string) (EventTimePolicy, bool) {
	family := MetricFamilyForEventType(eventType)
	if family != "order_funnel" && family != "provider_supply" {
		return EventTimePolicy{}, false
	}
	return EventTimePolicy{MetricFamily: family, Version: EventTimePolicyVersion, AllowedLateness: DefaultAllowedLateness}, true
}

// ClassifyEventTime is deliberately independent of wall-clock time. Replays
// use the persisted outbox created_at timestamp and therefore reproduce the
// same classification. Future source timestamps are clock-skew anomalies;
// no arbitrary skew tolerance is invented.
func ClassifyEventTime(occurredAt, ingestedAt time.Time, policy EventTimePolicy) (EventTimeClassification, error) {
	if occurredAt.IsZero() || ingestedAt.IsZero() || policy.AllowedLateness < 0 || policy.MetricFamily == "" || policy.Version == "" {
		return EventTimeClassification{}, ErrInvalidEventTime
	}
	if occurredAt.After(ingestedAt) {
		return EventTimeClassification{ClockSkew: true}, nil
	}
	return EventTimeClassification{Late: occurredAt.Before(ingestedAt.Add(-policy.AllowedLateness))}, nil
}

// EventTimeWatermark is based on ingestion clock, never an outbox sequence or
// the maximum observed occurred_at. A period [from,to) is mature when to is
// at or before this watermark.
func EventTimeWatermark(now time.Time, policy EventTimePolicy) time.Time {
	return now.UTC().Add(-policy.AllowedLateness)
}

// IsPeriodMature uses an exclusive period end: equality with the watermark is
// finalizable, while any end after the watermark remains provisional.
func IsPeriodMature(periodEnd, watermark time.Time) bool {
	return !periodEnd.IsZero() && !watermark.IsZero() && !periodEnd.After(watermark)
}
