package marketplace

import (
	"errors"
	"math"
	"testing"

	"github.com/mojtaba-arvin/ai-service-marketplace/apps/api/internal/adapters"
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
