package marketconfig

import ("errors";"testing")

func validConfigs() []Config { return []Config{
 {MarketID:"OM",Locale:"en-OM",Currency:"OMR",TimeZone:"Asia/Muscat",PolicyVersion:"om-2026-01"},
 {MarketID:"OM",Locale:"ar-OM",Currency:"OMR",TimeZone:"Asia/Muscat",PolicyVersion:"om-2026-01"},
 {MarketID:"DE",Locale:"de-DE",Currency:"EUR",TimeZone:"Europe/Berlin",PolicyVersion:"de-2026-01"},
} }

func TestResolveRequiresExactMarketAndLocale(t *testing.T) {
 r,err:=New(validConfigs());if err!=nil{t.Fatal(err)}
 got,err:=r.Resolve("OM","en-OM");if err!=nil{t.Fatal(err)}
 if got.Currency!="OMR"||got.TimeZone!="Asia/Muscat"||got.PolicyVersion!="om-2026-01"{t.Fatalf("unexpected config: %+v",got)}
 for _,pair:=range [][2]string{{"OM","fr-OM"},{"om","en-OM"},{"XX","en-OM"},{"OM",""}} {
  if _,err:=r.Resolve(pair[0],pair[1]);!errors.Is(err,ErrUnknownMarket){t.Errorf("Resolve(%q,%q) error = %v",pair[0],pair[1],err)}
 }
}
func TestNewRejectsInvalidAndDuplicateEntries(t *testing.T) {
 base:=validConfigs()[0]
 cases:=map[string][]Config{
  "empty":nil,
  "invalid market":{{MarketID:"om",Locale:"en-OM",Currency:"OMR",TimeZone:"Asia/Muscat",PolicyVersion:"v1"}},
  "invalid locale":{{MarketID:"OM",Locale:"english",Currency:"OMR",TimeZone:"Asia/Muscat",PolicyVersion:"v1"}},
  "invalid currency":{{MarketID:"OM",Locale:"en-OM",Currency:"omr",TimeZone:"Asia/Muscat",PolicyVersion:"v1"}},
  "invalid timezone":{{MarketID:"OM",Locale:"en-OM",Currency:"OMR",TimeZone:"Mars/Olympus",PolicyVersion:"v1"}},
  "missing policy":{{MarketID:"OM",Locale:"en-OM",Currency:"OMR",TimeZone:"Asia/Muscat"}},
  "duplicate":{base,base},
 }
 for name,configs:=range cases {t.Run(name,func(t *testing.T){if _,err:=New(configs);!errors.Is(err,ErrInvalidConfig){t.Fatalf("got %v",err)}})}
}
func TestRegistryDoesNotExposeMutableInternalState(t *testing.T) {
 configs:=validConfigs();r,err:=New(configs);if err!=nil{t.Fatal(err)}
 configs[0].Currency="USD"
 got,err:=r.Resolve("OM","en-OM");if err!=nil{t.Fatal(err)}
 if got.Currency!="OMR"{t.Fatalf("registry changed through caller slice: %+v",got)}
 got.Currency="USD"
 again,err:=r.Resolve("OM","en-OM");if err!=nil{t.Fatal(err)}
 if again.Currency!="OMR"{t.Fatalf("registry changed through returned value: %+v",again)}
}
func TestNilRegistryFailsClosed(t *testing.T) {
 var r *Registry
 if _,err:=r.Resolve("OM","en-OM");!errors.Is(err,ErrUnknownMarket){t.Fatalf("got %v",err)}
}
