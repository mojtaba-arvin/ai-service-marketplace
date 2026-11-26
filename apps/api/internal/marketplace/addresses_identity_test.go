package marketplace

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/mojtaba-arvin/ai-service-marketplace/apps/api/internal/adapters"
)

func TestValidateExternalIdentityAssertion(t *testing.T) {
	now:=time.Now()
	valid:=adapters.VerifiedExternalIdentity{Provider:adapters.ProviderGoogle,Subject:"opaque-provider-subject",Nonce:"single-use-nonce",IssuedAt:now.Add(-time.Minute),ExpiresAt:now.Add(time.Minute)}
	if err:=validateExternalAssertion(valid,now);err!=nil{t.Fatalf("valid assertion rejected: %v",err)}
	cases:=[]adapters.VerifiedExternalIdentity{
		{Provider:"unknown",Subject:"subject",Nonce:"nonce",IssuedAt:now.Add(-time.Minute),ExpiresAt:now.Add(time.Minute)},
		{Provider:adapters.ProviderApple,Subject:"subject",Nonce:"nonce",IssuedAt:now.Add(-2*time.Minute),ExpiresAt:now.Add(-time.Minute)},
		{Provider:adapters.ProviderEmail,Subject:"subject",Nonce:"nonce",ExpiresAt:now.Add(time.Minute)},
		{Provider:adapters.ProviderGoogle,Subject:strings.Repeat("s",256),Nonce:"nonce",IssuedAt:now.Add(-time.Minute),ExpiresAt:now.Add(time.Minute)},
	}
	for _,assertion:=range cases{if !errors.Is(validateExternalAssertion(assertion,now),ErrExternalIdentityInvalid){t.Errorf("invalid assertion accepted: %#v",assertion)}}
}
