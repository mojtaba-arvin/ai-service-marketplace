package adapters

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestGeocodeUsesStableKeyAndValidatesProviderResult(t *testing.T) {
	fake := &FakeGeocoder{Results: map[string]GeocodeResult{
		"1 Example Street": {FormattedAddress:"1 Example Street, Muscat", Latitude:23.6, Longitude:58.4, Provider:"fake-maps", ProviderVersion:"test-v1"},
	}}
	runner := Runner{Policy:Policy{Timeout:time.Second,MaxAttempts:1,Retryable:func(error)bool{return false}}}
	got,err:=Geocode(context.Background(),runner,fake,GeocodeRequest{MarketID:"OM",Locale:"en-OM",Address:"1 Example Street",IdempotencyKey:"address:123"})
	if err!=nil {t.Fatal(err)}
	if got.Provider!="fake-maps"||fake.LastKey!="address:123"||fake.Calls!=1 {t.Fatalf("unexpected result/call: %#v %#v",got,fake)}
}

func TestGeocodeFailsClosed(t *testing.T) {
	runner:=Runner{Policy:Policy{Timeout:time.Second,MaxAttempts:1,Retryable:func(error)bool{return false}}}
	_,err:=Geocode(context.Background(),runner,&FakeGeocoder{},GeocodeRequest{MarketID:"OM",Locale:"en-OM",Address:"unknown",IdempotencyKey:"key"})
	if !errors.Is(err,ErrGeocoderUnavailable){t.Fatalf("got %v",err)}
	_,err=Geocode(context.Background(),runner,&FakeGeocoder{},GeocodeRequest{MarketID:"OM",Locale:"en-OM",Address:"x"})
	if !errors.Is(err,ErrInvalidGeocodeRequest){t.Fatalf("got %v",err)}
}
