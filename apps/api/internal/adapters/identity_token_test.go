package adapters

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

type staticSigningKey struct{ key SigningKey }
func (s staticSigningKey) ResolveSigningKey(IdentityProvider,string)(SigningKey,error){return s.key,nil}

func TestJWTIdentityVerifierValidatesSignatureAndClaims(t *testing.T) {
	private,err:=rsa.GenerateKey(rand.Reader,2048);if err!=nil{t.Fatal(err)}
	now:=time.Unix(1_800_000_000,0)
	header,_:=json.Marshal(jwtHeader{Algorithm:"RS256",KeyID:"key-1"})
	claims,_:=json.Marshal(jwtClaims{Issuer:"https://accounts.example.test",Audience:json.RawMessage(`["marketplace-client","secondary-client"]`),AuthorizedParty:"marketplace-client",Subject:"provider-subject",Nonce:"one-time-state",IssuedAt:now.Add(-time.Minute).Unix(),ExpiresAt:now.Add(time.Minute).Unix()})
	enc:=base64.RawURLEncoding.EncodeToString
	unsigned:=enc(header)+"."+enc(claims)
	digest:=sha256.Sum256([]byte(unsigned))
	sig,err:=rsa.SignPKCS1v15(rand.Reader,private,crypto.SHA256,digest[:]);if err!=nil{t.Fatal(err)}
	token:=unsigned+"."+enc(sig)
	verifier:=JWTIdentityVerifier{Policy:IdentityTokenPolicy{Provider:ProviderGoogle,Issuer:"https://accounts.example.test",Audience:"marketplace-client",AllowedAlgorithms:[]string{"RS256"}},Keys:staticSigningKey{SigningKey{Algorithm:"RS256",RSA:&private.PublicKey}},Now:func()time.Time{return now}}
	got,err:=verifier.Verify(token);if err!=nil{t.Fatal(err)}
	if got.Subject!="provider-subject"||got.Nonce!="one-time-state"||got.Provider!=ProviderGoogle{t.Fatalf("unexpected identity: %#v",got)}
}

func TestJWTIdentityVerifierRejectsTamperingAndAudienceMismatch(t *testing.T) {
	private,_:=rsa.GenerateKey(rand.Reader,2048)
	now:=time.Now()
	enc:=base64.RawURLEncoding.EncodeToString
	header,_:=json.Marshal(jwtHeader{Algorithm:"RS256",KeyID:"key-1"})
	claims,_:=json.Marshal(jwtClaims{Issuer:"issuer",Audience:json.RawMessage(`"other-client"`),Subject:"subject",Nonce:"nonce",IssuedAt:now.Add(-time.Minute).Unix(),ExpiresAt:now.Add(time.Minute).Unix()})
	unsigned:=enc(header)+"."+enc(claims)
	digest:=sha256.Sum256([]byte(unsigned))
	sig,_:=rsa.SignPKCS1v15(rand.Reader,private,crypto.SHA256,digest[:])
	token:=unsigned+"."+enc(sig)
	v:=JWTIdentityVerifier{Policy:IdentityTokenPolicy{Provider:ProviderApple,Issuer:"issuer",Audience:"expected-client",AllowedAlgorithms:[]string{"RS256"}},Keys:staticSigningKey{SigningKey{Algorithm:"RS256",RSA:&private.PublicKey}}}
	if _,err:=v.Verify(token);!errors.Is(err,ErrInvalidIdentityToken){t.Fatalf("audience mismatch accepted: %v",err)}
	tampered:=unsigned+"."+enc(sig)+"x"
	if _,err:=v.Verify(tampered);!errors.Is(err,ErrInvalidIdentityToken){t.Fatalf("tampered token accepted: %v",err)}
}

type unavailableSigningKey struct{}
func (unavailableSigningKey) ResolveSigningKey(IdentityProvider,string)(SigningKey,error) {
	return SigningKey{},ErrIdentityKeyUnavailable
}

func TestJWTIdentityVerifierFailsClosedWhenProviderKeysAreUnavailable(t *testing.T) {
	enc:=base64.RawURLEncoding.EncodeToString
	header,_:=json.Marshal(jwtHeader{Algorithm:"RS256",KeyID:"rotated-key"})
	claims,_:=json.Marshal(jwtClaims{Issuer:"issuer",Audience:json.RawMessage(`"client"`),Subject:"subject",Nonce:"nonce",IssuedAt:time.Now().Add(-time.Minute).Unix(),ExpiresAt:time.Now().Add(time.Minute).Unix()})
	token:=enc(header)+"."+enc(claims)+"."+enc([]byte("signature"))
	verifier:=JWTIdentityVerifier{Policy:IdentityTokenPolicy{Provider:ProviderGoogle,Issuer:"issuer",Audience:"client",AllowedAlgorithms:[]string{"RS256"}},Keys:unavailableSigningKey{}}
	if _,err:=verifier.Verify(token);!errors.Is(err,ErrIdentityKeyUnavailable){t.Fatalf("provider key outage did not fail closed: %v",err)}
}
