package adapters

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

type scriptedGeocoder struct {
	calls int
	keys []string
	err error
}

func (g *scriptedGeocoder) AdapterID() string { return "scripted" }

func (g *scriptedGeocoder) Geocode(_ context.Context, request GeocodeRequest) (GeocodeResult, error) {
	g.calls++
	g.keys = append(g.keys, request.IdempotencyKey)
	if g.err != nil {
		return GeocodeResult{}, g.err
	}
	if g.calls == 1 {
		return GeocodeResult{}, errors.New("temporary provider failure")
	}
	return GeocodeResult{FormattedAddress:"Muscat",Latitude:23.6,Longitude:58.4,Provider:"scripted",ProviderVersion:"test-v1"}, nil
}

func TestGeocodeUsesStableKeyAndValidatesProviderResult(t *testing.T) {
	fake := &FakeGeocoder{Results: map[string]GeocodeResult{
		"1 Example Street": {FormattedAddress:"1 Example Street, Muscat", Latitude:23.6, Longitude:58.4, Provider:"fake-maps", ProviderVersion:"test-v1"},
	}}
	runner := Runner{Policy:Policy{Timeout:time.Second,MaxAttempts:1,Retryable:func(error)bool{return false}}}
	got,err:=Geocode(context.Background(),runner,fake,GeocodeRequest{MarketID:"OM",Locale:"en-OM",Address:"1 Example Street",IdempotencyKey:"address:123"})
	if err!=nil {t.Fatal(err)}
	if got.Provider!="fake-maps"||fake.LastKey!="address:123"||fake.Calls!=1 {t.Fatalf("unexpected result/call: %#v %#v",got,fake)}
}

func TestGeocodeRetriesWithSameIdempotencyKey(t *testing.T) {
	provider:=&scriptedGeocoder{}
	runner:=Runner{Policy:Policy{Timeout:time.Second,MaxAttempts:2,Retryable:func(err error)bool{return err!=nil}}}
	_,err:=Geocode(context.Background(),runner,provider,GeocodeRequest{MarketID:"OM",Locale:"en-OM",Address:"Muscat",IdempotencyKey:"address:retry-1"})
	if err!=nil {t.Fatal(err)}
	if provider.calls!=2 || len(provider.keys)!=2 || provider.keys[0]!="address:retry-1" || provider.keys[1]!="address:retry-1" {
		t.Fatalf("retry did not preserve the idempotency key: %#v",provider)
	}
}

func TestGeocodeProviderFailureDoesNotLeakRequestData(t *testing.T) {
	privateAddress:="17 Secret Customer Road"
	provider:=&scriptedGeocoder{err:errors.New("provider rejected address: "+privateAddress)}
	runner:=Runner{Policy:Policy{Timeout:time.Second,MaxAttempts:1,Retryable:func(error)bool{return false}}}
	_,err:=Geocode(context.Background(),runner,provider,GeocodeRequest{MarketID:"OM",Locale:"en-OM",Address:privateAddress,IdempotencyKey:"address:private-1"})
	if !errors.Is(err,ErrGeocoderUnavailable) {t.Fatalf("expected sanitized provider failure, got %v",err)}
	if strings.Contains(err.Error(),privateAddress) {t.Fatalf("provider error leaked private address: %v",err)}
}

func TestGeocodeFailsClosed(t *testing.T) {
	runner:=Runner{Policy:Policy{Timeout:time.Second,MaxAttempts:1,Retryable:func(error)bool{return false}}}
	_,err:=Geocode(context.Background(),runner,&FakeGeocoder{},GeocodeRequest{MarketID:"OM",Locale:"en-OM",Address:"unknown",IdempotencyKey:"key"})
	if !errors.Is(err,ErrGeocoderUnavailable){t.Fatalf("got %v",err)}
	_,err=Geocode(context.Background(),runner,&FakeGeocoder{},GeocodeRequest{MarketID:"OM",Locale:"en-OM",Address:"x"})
	if !errors.Is(err,ErrInvalidGeocodeRequest){t.Fatalf("got %v",err)}
}
