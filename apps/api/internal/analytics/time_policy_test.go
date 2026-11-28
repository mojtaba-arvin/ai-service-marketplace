package analytics

import (
	"testing"
	"time"
)

func TestEventTimePolicyFamilies(t *testing.T) {
	for eventType, family := range map[string]string{
		"order.created": "order_funnel",
		"dispatch.offer_created": "order_funnel",
		"provider.verified": "provider_supply",
	} {
		policy, ok := EventTimePolicyFor(eventType)
		if !ok || policy.MetricFamily != family || policy.AllowedLateness != 72*time.Hour || policy.Version == "" {
			t.Fatalf("unexpected policy for %s: %+v ok=%v", eventType, policy, ok)
		}
	}
	for eventType, family := range map[string]string{
		"review.published":"service_quality",
		"trust.dispute_finalized":"service_quality",
		"finance.journal_posted":"finance",
		"ledger.reversed":"finance",
	} {
		if got:=MetricFamilyForEventType(eventType); got!=family {
			t.Fatalf("%s mapped to %q, want %q",eventType,got,family)
		}
		if _, ok := EventTimePolicyFor(eventType); ok {
			t.Fatalf("%s must fail closed until its family-specific lateness/maturity policy is defined", eventType)
		}
	}
	if MetricFamilyForEventType("unknown.event")!="" { t.Fatal("unknown event must not acquire a metric family") }
}

func TestClassifyEventTimeBoundariesAndClockSkew(t *testing.T) {
	ingested := time.Date(2026, 11, 28, 12, 0, 0, 0, time.UTC)
	policy, _ := EventTimePolicyFor("order.created")
	tests := []struct {
		name string
		occurred time.Time
		late bool
		skew bool
	}{
		{name:"exact lateness boundary is accepted", occurred:ingested.Add(-72*time.Hour)},
		{name:"one nanosecond beyond boundary is late", occurred:ingested.Add(-72*time.Hour-time.Nanosecond), late:true},
		{name:"same instant is on time", occurred:ingested},
		{name:"future source clock is skew", occurred:ingested.Add(time.Nanosecond), skew:true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ClassifyEventTime(tt.occurred, ingested, policy)
			if err != nil || got.Late != tt.late || got.ClockSkew != tt.skew {
				t.Fatalf("classification=%+v err=%v, want late=%v skew=%v", got, err, tt.late, tt.skew)
			}
		})
	}
}

func TestEventTimeReplayIsDeterministicAndWatermarkUsesIngestionClock(t *testing.T) {
	ingested := time.Date(2026, 11, 28, 12, 0, 0, 0, time.UTC)
	occurred := ingested.Add(-73 * time.Hour)
	policy, _ := EventTimePolicyFor("order.created")
	first, err := ClassifyEventTime(occurred, ingested, policy)
	if err != nil { t.Fatal(err) }
	replayed, err := ClassifyEventTime(occurred, ingested, policy)
	if err != nil || replayed != first || !replayed.Late {
		t.Fatalf("replay changed classification: first=%+v replay=%+v err=%v", first, replayed, err)
	}
	watermark := EventTimeWatermark(ingested, policy)
	if !watermark.Equal(ingested.Add(-72*time.Hour)) {
		t.Fatalf("watermark=%s", watermark)
	}
	if !IsPeriodMature(watermark, watermark) {
		t.Fatal("period ending exactly at watermark must be finalizable")
	}
	if IsPeriodMature(watermark.Add(time.Nanosecond), watermark) {
		t.Fatal("period ending after watermark must remain provisional")
	}
}

func TestEventTimeCompletenessStateMachine(t *testing.T) {
	watermark:=time.Date(2026,11,28,0,0,0,0,time.UTC)
	periodEnd:=watermark
	cases:=[]struct{
		name string
		pending,quarantined,untrusted,duplicateConflicts,late,lateIncluded,excluded,eligible int64
		end time.Time
		want string
	}{
		{name:"finalized at exact watermark",eligible:4,end:periodEnd,want:"finalized"},
		{name:"period after watermark",eligible:4,end:periodEnd.Add(time.Nanosecond),want:"immature"},
		{name:"late arrival requires correction",late:1,eligible:4,end:periodEnd,want:"correction_required"},
		{name:"explicit late correction",late:1,lateIncluded:1,eligible:4,end:periodEnd,want:"corrected"},
		{name:"duplicate ordering correction",duplicateConflicts:1,eligible:4,end:periodEnd,want:"correction_required"},
		{name:"pending ingestion",pending:1,eligible:4,end:periodEnd,want:"incomplete"},
		{name:"quarantine",quarantined:1,eligible:4,end:periodEnd,want:"incomplete"},
		{name:"untrusted ingestion timestamp",untrusted:1,eligible:4,end:periodEnd,want:"incomplete"},
		{name:"excluded source metadata",excluded:1,eligible:4,end:periodEnd,want:"incomplete"},
		{name:"no accepted cohort",end:periodEnd,want:"unavailable"},
	}
	for _,tc:=range cases{
		t.Run(tc.name,func(t *testing.T){
			got:=EventTimeCompleteness(tc.end,watermark,tc.pending,tc.quarantined,tc.untrusted,tc.duplicateConflicts,tc.late,tc.lateIncluded,tc.excluded,tc.eligible)
			if got!=tc.want{t.Fatalf("status=%q want %q",got,tc.want)}
		})
	}
}
