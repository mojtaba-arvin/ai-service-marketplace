package adapters

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/asn1"
	"encoding/base64"
	"encoding/json"
	"errors"
	"math/big"
	"strings"
	"time"
)

var (
	ErrInvalidIdentityToken = errors.New("identity provider assertion is invalid")
	ErrIdentityKeyUnavailable = errors.New("identity provider signing key is unavailable")
)

type IdentityProvider string

const (
	ProviderApple IdentityProvider = "apple"
	ProviderGoogle IdentityProvider = "google"
	ProviderEmail IdentityProvider = "email"
)

type SigningKey struct {
	Algorithm string
	RSA *rsa.PublicKey
	ECDSA *ecdsa.PublicKey
}

type SigningKeyResolver interface {
	ResolveSigningKey(provider IdentityProvider, keyID string) (SigningKey, error)
}

type IdentityTokenPolicy struct {
	Provider IdentityProvider
	Issuer string
	Audience string
	AllowedAlgorithms []string
}

type VerifiedExternalIdentity struct {
	Provider IdentityProvider
	Subject string
	Nonce string
	IssuedAt time.Time
	ExpiresAt time.Time
}

type IdentityTokenVerifier interface {
	Verify(string) (VerifiedExternalIdentity, error)
}

type JWTIdentityVerifier struct {
	Policy IdentityTokenPolicy
	Keys SigningKeyResolver
	Now func() time.Time
}

type jwtHeader struct { Algorithm string `json:"alg"`; KeyID string `json:"kid"` }
type jwtClaims struct {
	Issuer string `json:"iss"`
	Audience json.RawMessage `json:"aud"`
	Subject string `json:"sub"`
	ExpiresAt int64 `json:"exp"`
	IssuedAt int64 `json:"iat"`
	NotBefore int64 `json:"nbf,omitempty"
	AuthorizedParty string `json:"azp,omitempty"`
	Nonce string `json:"nonce"`
}

func (v JWTIdentityVerifier) Verify(token string) (VerifiedExternalIdentity,error) {
	if v.Keys==nil || (v.Policy.Provider!=ProviderApple && v.Policy.Provider!=ProviderGoogle) ||
		v.Policy.Issuer=="" || v.Policy.Audience=="" || strings.TrimSpace(token)!=token { return VerifiedExternalIdentity{},ErrInvalidIdentityToken }
	parts:=strings.Split(token,".")
	if len(parts)!=3 {return VerifiedExternalIdentity{},ErrInvalidIdentityToken}
	decode:=func(s string)([]byte,error){return base64.RawURLEncoding.DecodeString(s)}
	headerBytes,err:=decode(parts[0]);if err!=nil{return VerifiedExternalIdentity{},ErrInvalidIdentityToken}
	var header jwtHeader
	if json.Unmarshal(headerBytes,&header)!=nil || header.KeyID=="" || !v.allowed(header.Algorithm) {return VerifiedExternalIdentity{},ErrInvalidIdentityToken}
	key,err:=v.Keys.ResolveSigningKey(v.Policy.Provider,header.KeyID)
	if err!=nil{return VerifiedExternalIdentity{},ErrIdentityKeyUnavailable}
	signature,err:=decode(parts[2]);if err!=nil{return VerifiedExternalIdentity{},ErrInvalidIdentityToken}
	digest:=sha256.Sum256([]byte(parts[0]+"."+parts[1]))
	if !verifyJWTSignature(header.Algorithm,key,digest[:],signature){return VerifiedExternalIdentity{},ErrInvalidIdentityToken}
	claimBytes,err:=decode(parts[1]);if err!=nil{return VerifiedExternalIdentity{},ErrInvalidIdentityToken}
	var claims jwtClaims
	if json.Unmarshal(claimBytes,&claims)!=nil || claims.Issuer!=v.Policy.Issuer || claims.Subject=="" || claims.Nonce=="" {return VerifiedExternalIdentity{},ErrInvalidIdentityToken}
	var audience string
	audienceCount:=1
	if len(claims.Audience)>0 && claims.Audience[0]=='"' { _=json.Unmarshal(claims.Audience,&audience) } else {
		var values []string
		if json.Unmarshal(claims.Audience,&values)!=nil {return VerifiedExternalIdentity{},ErrInvalidIdentityToken}
		audienceCount=len(values)
		for _,value:=range values {if value==v.Policy.Audience {audience=value;break}}
	}
	if audience!=v.Policy.Audience || (audienceCount>1 && claims.AuthorizedParty!=v.Policy.Audience) {return VerifiedExternalIdentity{},ErrInvalidIdentityToken}
	now:=time.Now()
	if v.Now!=nil {now=v.Now()}
	issued:=time.Unix(claims.IssuedAt,0)
	expires:=time.Unix(claims.ExpiresAt,0)
	if claims.IssuedAt<=0 || claims.ExpiresAt<=claims.IssuedAt || !now.Before(expires) || issued.After(now) || (claims.NotBefore>0 && now.Before(time.Unix(claims.NotBefore,0))) {return VerifiedExternalIdentity{},ErrInvalidIdentityToken}
	return VerifiedExternalIdentity{Provider:v.Policy.Provider,Subject:claims.Subject,Nonce:claims.Nonce,IssuedAt:issued,ExpiresAt:expires},nil
}

func (v JWTIdentityVerifier) allowed(algorithm string) bool {
	for _,candidate:=range v.Policy.AllowedAlgorithms {if candidate==algorithm{return true}}
	return false
}

func verifyJWTSignature(algorithm string,key SigningKey,digest,signature []byte) bool {
	switch algorithm {
	case "RS256":
		return key.Algorithm==algorithm && key.RSA!=nil && rsa.VerifyPKCS1v15(key.RSA,crypto.SHA256,digest,signature)==nil
	case "ES256":
		if key.Algorithm!=algorithm || key.ECDSA==nil || len(signature)!=64{return false}
		r:=new(big.Int).SetBytes(signature[:32]);s:=new(big.Int).SetBytes(signature[32:])
		der,err:=asn1.Marshal(struct{R,S *big.Int}{r,s});if err!=nil{return false}
		return ecdsa.VerifyASN1(key.ECDSA,digest,der)
	default:
		return false
	}
}
