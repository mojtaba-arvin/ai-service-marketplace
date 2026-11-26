package marketplace

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"

	"github.com/mojtaba-arvin/ai-service-marketplace/apps/api/internal/adapters"
	"github.com/mojtaba-arvin/ai-service-marketplace/apps/api/internal/marketconfig"
)

func TestSavedAddressValidation(t *testing.T) {
	valid:=SavedAddressInput{Label:"Home",AddressLine1:"1 Example Street",Locality:"Muscat",CountryCode:"OM"}
	if err:=validateSavedAddress(valid);err!=nil{t.Fatalf("valid address rejected: %v",err)}
	cases:=[]SavedAddressInput{
		{Label:"",AddressLine1:"1 Example Street",Locality:"Muscat",CountryCode:"OM"},
		{Label:"Home",AddressLine1:" ",Locality:"Muscat",CountryCode:"OM"},
		{Label:"Home",AddressLine1:"1 Example Street",Locality:"Muscat",CountryCode:"om"},
		{Label:" Home",AddressLine1:"1 Example Street",Locality:"Muscat",CountryCode:"OM"},
	}
	for _,input:=range cases{if !errors.Is(validateSavedAddress(input),ErrInvalidAddress){t.Errorf("invalid input accepted: %#v",input)}}
}

func TestSavedAddressAuthorizationIsCustomerSelfOnly(t *testing.T) {
	if err:=validateAddressActor(Actor{SubjectID:"identity-1",TenantID:"tenant-1",Role:RoleCustomer});err!=nil{t.Fatal(err)}
	for _,actor:=range []Actor{
		{SubjectID:"identity-1",TenantID:"tenant-1",Role:RoleProfessional},
		{SubjectID:"",TenantID:"tenant-1",Role:RoleCustomer},
		{SubjectID:"identity-1",TenantID:"",Role:RoleCustomer},
	}{if !errors.Is(validateAddressActor(actor),ErrAddressForbidden){t.Errorf("actor accepted: %#v",actor)}}
}

func TestGeocodeResultRejectsInvalidCoordinates(t *testing.T) {
	valid:=&adapters.GeocodeResult{FormattedAddress:"Muscat",Latitude:23.6,Longitude:58.4,Provider:"maps",ProviderVersion:"v1"}
	if err:=validateGeocodeResult(valid);err!=nil{t.Fatal(err)}
	invalid:=*valid;invalid.Latitude=math.NaN()
	if !errors.Is(validateGeocodeResult(&invalid),ErrInvalidAddress){t.Fatal("NaN coordinates accepted")}
	invalid=*valid;invalid.Longitude=181
	if !errors.Is(validateGeocodeResult(&invalid),ErrInvalidAddress){t.Fatal("out-of-range coordinates accepted")}
}

func TestGeocodeSavedAddressRequiresConfiguredAdapterCapability(t *testing.T) {
	registry,err:=marketconfig.New([]marketconfig.Config{{MarketID:"OM",Locale:"en-OM",Currency:"OMR",TimeZone:"Asia/Muscat",PolicyVersion:"om-2026-01"}})
	if err!=nil{t.Fatal(err)}
	matrix,err:=marketconfig.NewMatrix(registry,[]marketconfig.AdapterConfig{{MarketID:"OM",Locale:"en-OM",PolicyVersion:"om-2026-01",Adapter:"maps",Capabilities:[]string{"geocode"}}})
	if err!=nil{t.Fatal(err)}
	fake:=&adapters.FakeGeocoder{ID:"maps",Results:map[string]adapters.GeocodeResult{"Muscat":{FormattedAddress:"Muscat",Latitude:23.6,Longitude:58.4,Provider:"fake",ProviderVersion:"v1"}}}
	runner:=adapters.Runner{Policy:adapters.Policy{Timeout:time.Second,MaxAttempts:1,Retryable:func(error)bool{return false}}}
	if _,err=GeocodeSavedAddress(context.Background(),matrix,"OM","en-OM",runner,fake,"Muscat","address:1");err!=nil{t.Fatalf("configured geocoder rejected: %v",err)}
	disabled,err:=marketconfig.NewMatrix(registry,[]marketconfig.AdapterConfig{{MarketID:"OM",Locale:"en-OM",PolicyVersion:"om-2026-01",Adapter:"maps",Capabilities:[]string{}}})
	if err!=nil{t.Fatal(err)}
	if _,err=GeocodeSavedAddress(context.Background(),disabled,"OM","en-OM",runner,fake,"Muscat","address:2");!errors.Is(err,marketconfig.ErrUnknownCapability){t.Fatalf("disabled geocoding capability accepted: %v",err)}
}
