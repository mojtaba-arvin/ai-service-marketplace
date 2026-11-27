package marketconfig

import (
	"errors"
	"testing"
)

func TestDirectionFromLocale(t *testing.T) {
	tests := []struct { locale string; want Direction }{
		{"en-OM", DirectionLTR}, {"de-DE", DirectionLTR},
		{"ar-OM", DirectionRTL}, {"fa-IR", DirectionRTL},
		{"he-IL", DirectionRTL}, {"ur-PK", DirectionRTL},
	}
	for _, tt := range tests {
		t.Run(tt.locale, func(t *testing.T) {
			got, err := (Config{Locale: tt.locale}).Direction()
			if err != nil || got != tt.want { t.Fatalf("Direction() = %q, %v; want %q", got, err, tt.want) }
		})
	}
	if _, err := (Config{Locale: "english"}).Direction(); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("invalid locale error = %v", err)
	}
}

func TestCapabilityMatrixRequiresExactMarketLocaleAndCapability(t *testing.T) {
	registry, err := New(validConfigs()); if err != nil { t.Fatal(err) }
	matrix, err := NewMatrix(registry, []AdapterConfig{
		{MarketID:"OM", Locale:"en-OM", PolicyVersion:"om-2026-01", Adapter:"maps", Capabilities:[]string{"geocode","route"}},
		{MarketID:"OM", Locale:"ar-OM", PolicyVersion:"om-2026-01", Adapter:"maps", Capabilities:[]string{"geocode"}},
		{MarketID:"DE", Locale:"de-DE", PolicyVersion:"de-2026-01", Adapter:"maps", Capabilities:[]string{"geocode","route"}},
	})
	if err != nil { t.Fatal(err) }
	if err := matrix.Require("OM","en-OM","maps","route"); err != nil { t.Fatalf("configured capability rejected: %v",err) }
	if err := matrix.Require("OM","ar-OM","maps","route"); !errors.Is(err,ErrUnknownCapability) { t.Fatalf("unsupported capability error = %v",err) }
	if err := matrix.Require("OM","ar-OM","sms","send"); !errors.Is(err,ErrAdapterUnavailable) { t.Fatalf("unavailable adapter error = %v",err) }
	if err := matrix.Require("OM","fr-OM","maps","geocode"); !errors.Is(err,ErrUnknownMarket) { t.Fatalf("unknown locale error = %v",err) }
	if err := matrix.Require("XX","en-OM","maps","geocode"); !errors.Is(err,ErrUnknownMarket) { t.Fatalf("unknown market error = %v",err) }
}

func TestNewMatrixRejectsUnknownMarketsDuplicateRowsAndVersions(t *testing.T) {
	registry, err := New(validConfigs()); if err != nil { t.Fatal(err) }
	tests := []struct { name string; rows []AdapterConfig; want error }{
		{"unknown market", []AdapterConfig{{MarketID:"XX",Locale:"en-OM",Adapter:"maps"}}, ErrUnknownMarket},
		{"policy version mismatch", []AdapterConfig{{MarketID:"OM",Locale:"en-OM",PolicyVersion:"old",Adapter:"maps"}}, ErrInvalidConfig},
		{"duplicate row", []AdapterConfig{
			{MarketID:"OM",Locale:"en-OM",PolicyVersion:"om-2026-01",Adapter:"maps"},
			{MarketID:"OM",Locale:"en-OM",PolicyVersion:"om-2026-01",Adapter:"maps"},
		}, ErrInvalidConfig},
		{"duplicate capability", []AdapterConfig{{
			MarketID:"OM",Locale:"en-OM",PolicyVersion:"om-2026-01",Adapter:"maps",Capabilities:[]string{"route","route"},
		}}, ErrInvalidConfig},
		{"invalid capability", []AdapterConfig{{
			MarketID:"OM",Locale:"en-OM",PolicyVersion:"om-2026-01",Adapter:"maps",Capabilities:[]string{"Route"},
		}}, ErrInvalidConfig},
	}
	for _, tt := range tests {
		t.Run(tt.name,func(t *testing.T){
			if _,err:=NewMatrix(registry,tt.rows); !errors.Is(err,tt.want) { t.Fatalf("NewMatrix() error = %v, want %v",err,tt.want) }
		})
	}
}

func TestCapabilityMatrixDoesNotExposeMutableInput(t *testing.T) {
	registry,err:=New(validConfigs()); if err!=nil {t.Fatal(err)}
	capabilities:=[]string{"geocode"}
	rows:=[]AdapterConfig{{MarketID:"OM",Locale:"en-OM",PolicyVersion:"om-2026-01",Adapter:"maps",Capabilities:capabilities}}
	matrix,err:=NewMatrix(registry,rows);if err!=nil{t.Fatal(err)}
	capabilities[0]="route";rows[0].Adapter="other"
	if err:=matrix.Require("OM","en-OM","maps","geocode");err!=nil{t.Fatalf("matrix mutated through caller input: %v",err)}
	if err:=matrix.Require("OM","en-OM","maps","route");!errors.Is(err,ErrUnknownCapability){t.Fatalf("unexpected capability after caller mutation: %v",err)}
}
