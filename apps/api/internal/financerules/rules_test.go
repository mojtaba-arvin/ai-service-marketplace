package financerules

import (
	"errors"
	"math"
	"testing"
	"time"
)

func approvedRule() Rule {
	now:=time.Date(2026,1,1,0,0,0,0,time.UTC)
	approved:=now.Add(-time.Hour)
	return Rule{ID:"commission-om-v1",Version:1,MarketID:"OM",Currency:"OMR",Kind:Commission,Basis:Percentage,Rate:1250,Rounding:HalfUp,EffectiveFrom:now,Priority:10,Provenance:"approved configuration",ApprovalID:"approval-1",ApprovedAt:&approved}
}
func TestCalculateIntegerMinorUnitsAndRounding(t *testing.T) {
	r:=approvedRule(); at:=r.EffectiveFrom.Add(time.Hour)
	cases:=[]struct{basis,want int64}{{1000,125},{1004,126},{0,0}}
	for _,tc:=range cases {got,err:=Calculate(r,Input{MarketID:"OM",Currency:"OMR",At:at,BasisMinor:tc.basis,SourceID:"order-1"});if err!=nil||got.AmountMinor!=tc.want{t.Fatalf("basis %d: got %d err %v",tc.basis,got.AmountMinor,err)}}
}
func TestCalculateCapsFloorsAndFixedMinor(t *testing.T) {
	r:=approvedRule(); floor,max:=int64(50),int64(80);r.MinimumMinor=&floor;r.MaximumMinor=&max
	got,err:=Calculate(r,Input{MarketID:"OM",Currency:"OMR",At:r.EffectiveFrom,BasisMinor:10000,SourceID:"order"});if err!=nil||got.AmountMinor!=80{t.Fatalf("cap result=%+v err=%v",got,err)}
	r.Basis=FixedMinor;r.Rate=17
	got,err=Calculate(r,Input{MarketID:"OM",Currency:"OMR",At:r.EffectiveFrom,BasisMinor:0,SourceID:"order"});if err!=nil||got.AmountMinor!=50{t.Fatalf("floor result=%+v err=%v",got,err)}
}
func TestResolveEffectiveDateAndPrecedence(t *testing.T) {
	a:=approvedRule();b:=a;b.ID="commission-om-v2";b.Version=2;b.Priority=20;b.EffectiveFrom=a.EffectiveFrom.Add(24*time.Hour)
	at:=a.EffectiveFrom.Add(12*time.Hour)
	got,err:=Resolve([]Rule{b,a},"OM","OMR",at);if err!=nil||got.ID!=a.ID{t.Fatalf("historical resolve=%+v err=%v",got,err)}
	at=b.EffectiveFrom
	got,err=Resolve([]Rule{a,b},"OM","OMR",at);if err!=nil||got.ID!=b.ID{t.Fatalf("new resolve=%+v err=%v",got,err)}
}
func TestResolveFailsClosedForMarketCurrencyAndAmbiguousPriority(t *testing.T) {
	a:=approvedRule()
	if _,err:=Resolve([]Rule{a},"DE","OMR",a.EffectiveFrom);!errors.Is(err,ErrNoApplicableRule){t.Fatalf("market isolation: %v",err)}
	if _,err:=Resolve([]Rule{a},"OM","EUR",a.EffectiveFrom);!errors.Is(err,ErrNoApplicableRule){t.Fatalf("currency isolation: %v",err)}
	b:=a;b.ID="other-rule"
	if _,err:=Resolve([]Rule{a,b},"OM","OMR",a.EffectiveFrom);!errors.Is(err,ErrAmbiguousRule){t.Fatalf("precedence tie: %v",err)}
}
func TestCalculateRequiresApprovedExplicitProvenanceAndExactScope(t *testing.T) {
	r:=approvedRule();r.ApprovedAt=nil
	if err:=Validate(r);!errors.Is(err,ErrInvalidRule){t.Fatalf("unapproved rule: %v",err)}
	r=approvedRule()
	for _,in:=range []Input{{MarketID:"DE",Currency:"OMR",At:r.EffectiveFrom,SourceID:"x"},{MarketID:"OM",Currency:"EUR",At:r.EffectiveFrom,SourceID:"x"},{MarketID:"OM",Currency:"OMR",At:r.EffectiveFrom,SourceID:""}} {
		if _,err:=Calculate(r,in);!errors.Is(err,ErrInvalidInput){t.Fatalf("invalid input accepted: %v",err)}
	}
}
func TestCalculateRejectsOverflow(t *testing.T) {
	r:=approvedRule();r.Rate=10000
	if _,err:=Calculate(r,Input{MarketID:"OM",Currency:"OMR",At:r.EffectiveFrom,BasisMinor:math.MaxInt64,SourceID:"order"});err!=nil{t.Fatalf("100 percent of MaxInt64 should fit: %v",err)}
	r.Rate=10001
	if err:=Validate(r);!errors.Is(err,ErrInvalidRule){t.Fatalf("rate >100%% accepted: %v",err)}
}
func TestInputSnapshotHashIsDeterministic(t *testing.T) {
	r:=approvedRule();in:=Input{MarketID:"OM",Currency:"OMR",At:r.EffectiveFrom,BasisMinor:12345,SourceID:"order-1"}
	a,err:=Calculate(r,in);if err!=nil{t.Fatal(err)}
	b,err:=Calculate(r,in);if err!=nil{t.Fatal(err)}
	if a.InputHash==""||a.InputHash!=b.InputHash||string(a.RuleSnapshot)!=string(b.RuleSnapshot){t.Fatalf("unstable provenance: %+v %+v",a,b)}
}

func TestRoundingModesAndZeroOutcome(t *testing.T) {
	r:=approvedRule();r.Rate=1
	in:=Input{MarketID:"OM",Currency:"OMR",At:r.EffectiveFrom,BasisMinor:5000,SourceID:"order"}
	r.Rounding=HalfUp
	got,err:=Calculate(r,in);if err!=nil||got.AmountMinor!=1{t.Fatalf("half-up: %+v %v",got,err)}
	r.Rounding=Down
	got,err=Calculate(r,in);if err!=nil||got.AmountMinor!=0{t.Fatalf("down: %+v %v",got,err)}
	r.Rounding=Up
	got,err=Calculate(r,in);if err!=nil||got.AmountMinor!=1{t.Fatalf("up: %+v %v",got,err)}
}
func TestInputHashNormalizesEquivalentInstantsToUTC(t *testing.T) {
	r:=approvedRule()
	at:=r.EffectiveFrom.Add(time.Hour)
	a,err:=Calculate(r,Input{MarketID:"OM",Currency:"OMR",At:at,SourceID:"order",BasisMinor:100})
	if err!=nil{t.Fatal(err)}
	_,offset:=at.Zone()
	_ = offset
	other:=at.In(time.FixedZone("equivalent",3600))
	b,err:=Calculate(r,Input{MarketID:"OM",Currency:"OMR",At:other,SourceID:"order",BasisMinor:100})
	if err!=nil{t.Fatal(err)}
	if a.InputHash!=b.InputHash{t.Fatalf("equivalent instants produced different hashes: %s %s",a.InputHash,b.InputHash)}
}
