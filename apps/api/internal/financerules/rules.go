// Package financerules evaluates explicitly approved, market-scoped financial rules.
// It contains no default rates or legal/tax policy and never moves money.
package financerules

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/big"
	"sort"
	"strings"
	"time"
)

var (
	ErrInvalidRule = errors.New("invalid finance rule")
	ErrNoApplicableRule = errors.New("no approved finance rule applies")
	ErrAmbiguousRule = errors.New("finance rule precedence is ambiguous")
	ErrInvalidInput = errors.New("invalid finance calculation input")
	ErrOverflow = errors.New("finance calculation exceeds integer minor-unit range")
)

type Kind string
const (
	Commission Kind = "commission"
	Reward Kind = "reward"
	Credit Kind = "credit"
)
type Basis string
const (
	FixedMinor Basis = "fixed_minor"
	Percentage Basis = "percentage"
)
type Rounding string
const (
	HalfUp Rounding = "half_up"
	Down Rounding = "down"
	Up Rounding = "up"
)

type Rule struct {
	ID string `json:"id"`
	Version int `json:"version"`
	MarketID string `json:"market_id"`
	Currency string `json:"currency"`
	Kind Kind `json:"kind"`
	Basis Basis `json:"basis"`
	// Rate is an integer number of basis points for Percentage, or minor units
	// for FixedMinor. It is always adopter-supplied; zero is a valid explicit value.
	Rate int64 `json:"rate"`
	Rounding Rounding `json:"rounding"`
	EffectiveFrom time.Time `json:"effective_from"`
	EffectiveUntil *time.Time `json:"effective_until,omitempty"`
	Priority int `json:"priority"`
	MinimumMinor *int64 `json:"minimum_minor,omitempty"`
	MaximumMinor *int64 `json:"maximum_minor,omitempty"`
	Provenance string `json:"provenance"`
	ApprovalID string `json:"approval_id"`
	ApprovedAt *time.Time `json:"approved_at,omitempty"`
}

type Input struct {
	MarketID string `json:"market_id"`
	Currency string `json:"currency"`
	At time.Time `json:"at"`
	BasisMinor int64 `json:"basis_minor"`
	SourceID string `json:"source_id"`
}
type Result struct {
	RuleID string `json:"rule_id"`
	RuleVersion int `json:"rule_version"`
	MarketID string `json:"market_id"`
	Currency string `json:"currency"`
	Kind Kind `json:"kind"`
	BasisMinor int64 `json:"basis_minor"`
	AmountMinor int64 `json:"amount_minor"`
	InputHash string `json:"input_hash"`
	RuleSnapshot json.RawMessage `json:"rule_snapshot"`
}

func Validate(r Rule) error {
	if strings.TrimSpace(r.ID)=="" || len(r.ID)>120 || r.Version<1 ||
		!validMarket(r.MarketID) || !validCurrency(r.Currency) ||
		(r.Kind!=Commission && r.Kind!=Reward && r.Kind!=Credit) ||
		(r.Basis!=FixedMinor && r.Basis!=Percentage) || r.Rate<0 ||
		r.EffectiveFrom.IsZero() || r.Provenance=="" || r.ApprovalID=="" ||
		r.ApprovedAt==nil || r.ApprovedAt.IsZero() {
		return ErrInvalidRule
	}
	if r.Basis==Percentage && r.Rate>10000 { return ErrInvalidRule }
	if r.Rounding!=HalfUp && r.Rounding!=Down && r.Rounding!=Up { return ErrInvalidRule }
	if r.EffectiveUntil!=nil && !r.EffectiveUntil.After(r.EffectiveFrom) { return ErrInvalidRule }
	if r.MinimumMinor!=nil && *r.MinimumMinor<0 { return ErrInvalidRule }
	if r.MaximumMinor!=nil && *r.MaximumMinor<0 { return ErrInvalidRule }
	if r.MinimumMinor!=nil && r.MaximumMinor!=nil && *r.MinimumMinor>*r.MaximumMinor { return ErrInvalidRule }
	return nil
}
func validMarket(s string) bool { if len(s)<2||len(s)>8{return false}; for _,c:=range s {if c<'A'||c>'Z'{return false}}; return true }
func validCurrency(s string) bool { if len(s)!=3{return false}; for _,c:=range s {if c<'A'||c>'Z'{return false}}; return true }
func active(r Rule, at time.Time) bool { return !at.Before(r.EffectiveFrom) && (r.EffectiveUntil==nil || at.Before(*r.EffectiveUntil)) }

// Resolve requires exact market/currency scope. Highest priority wins; ties fail closed.
func Resolve(rules []Rule, market,currency string, at time.Time) (Rule,error) {
	if !validMarket(market)||!validCurrency(currency)||at.IsZero(){return Rule{},ErrInvalidInput}
	var eligible []Rule
	for _,r:=range rules {
		if err:=Validate(r); err!=nil { return Rule{},fmt.Errorf("%w: %v",ErrInvalidRule,err) }
		if r.MarketID==market && r.Currency==currency && active(r,at) { eligible=append(eligible,r) }
	}
	if len(eligible)==0{return Rule{},ErrNoApplicableRule}
	sort.Slice(eligible,func(i,j int)bool { if eligible[i].Priority!=eligible[j].Priority{return eligible[i].Priority>eligible[j].Priority}; if eligible[i].ID!=eligible[j].ID{return eligible[i].ID<eligible[j].ID}; return eligible[i].Version<eligible[j].Version })
	if len(eligible)>1 && eligible[0].Priority==eligible[1].Priority{return Rule{},ErrAmbiguousRule}
	return eligible[0],nil
}

func Calculate(r Rule, in Input) (Result,error) {
	if err:=Validate(r);err!=nil{return Result{},err}
	if in.MarketID!=r.MarketID||in.Currency!=r.Currency||in.At.IsZero()||in.SourceID==""||in.BasisMinor<0||!active(r,in.At){return Result{},ErrInvalidInput}
	var n *big.Int
	if r.Basis==FixedMinor { n=big.NewInt(r.Rate) } else {
		product:=new(big.Int).Mul(big.NewInt(in.BasisMinor),big.NewInt(r.Rate))
		denom:=big.NewInt(10000)
		q,rem:=new(big.Int).QuoRem(product,denom,new(big.Int))
		switch r.Rounding {
		case HalfUp: if new(big.Int).Mul(rem,big.NewInt(2)).Cmp(denom)>=0 {q.Add(q,big.NewInt(1))}
		case Up: if rem.Sign()>0 {q.Add(q,big.NewInt(1))}
		case Down:
		default: return Result{},ErrInvalidRule
		}
		n=q
	}
	if r.MinimumMinor!=nil && n.Cmp(big.NewInt(*r.MinimumMinor))<0 { n=big.NewInt(*r.MinimumMinor) }
	if r.MaximumMinor!=nil && n.Cmp(big.NewInt(*r.MaximumMinor))>0 { n=big.NewInt(*r.MaximumMinor) }
	if !n.IsInt64()||n.Sign()<0||n.Cmp(big.NewInt(math.MaxInt64))>0{return Result{},ErrOverflow}
	snapshot,err:=json.Marshal(r);if err!=nil{return Result{},err}
	canonicalInput:=in
	canonicalInput.At=in.At.UTC()
	inputBytes,err:=json.Marshal(canonicalInput);if err!=nil{return Result{},err}
	sum:=sha256.Sum256(inputBytes)
	return Result{RuleID:r.ID,RuleVersion:r.Version,MarketID:r.MarketID,Currency:r.Currency,Kind:r.Kind,BasisMinor:in.BasisMinor,AmountMinor:n.Int64(),InputHash:hex.EncodeToString(sum[:]),RuleSnapshot:snapshot},nil
}
