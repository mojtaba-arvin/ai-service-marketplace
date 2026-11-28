package adapters

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"math/big"
	"testing"
	"time"
)

type staticSigningKey struct{ key SigningKey }

func (s staticSigningKey) ResolveSigningKey(IdentityProvider, string) (SigningKey, error) {
	return s.key, nil
}

type unavailableSigningKey struct{}

func (unavailableSigningKey) ResolveSigningKey(IdentityProvider, string) (SigningKey, error) {
	return SigningKey{}, ErrIdentityKeyUnavailable
}

func signIdentityJWT(t *testing.T, algorithm, keyID string, private any, claims jwtClaims) string {
	t.Helper()
	header, err := json.Marshal(jwtHeader{Algorithm: algorithm, KeyID: keyID})
	if err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(claims)
	if err != nil {
		t.Fatal(err)
	}
	encode := base64.RawURLEncoding.EncodeToString
	unsigned := encode(header) + "." + encode(payload)
	digest := sha256.Sum256([]byte(unsigned))

	var signature []byte
	switch key := private.(type) {
	case *rsa.PrivateKey:
		signature, err = rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, digest[:])
	case *ecdsa.PrivateKey:
		var r, s *big.Int
		r, s, err = ecdsa.Sign(rand.Reader, key, digest[:])
		if err == nil {
			signature = make([]byte, 64)
			r.FillBytes(signature[:32])
			s.FillBytes(signature[32:])
		}
	default:
		t.Fatalf("unsupported test signing key %T", private)
	}
	if err != nil {
		t.Fatal(err)
	}
	return unsigned + "." + encode(signature)
}

func validClaims(now time.Time) jwtClaims {
	return jwtClaims{
		Issuer: "https://accounts.example.test",
		Audience: json.RawMessage(`"marketplace-client"`),
		Subject: "provider-subject",
		Nonce: "one-time-state",
		IssuedAt: now.Add(-time.Minute).Unix(),
		ExpiresAt: now.Add(time.Minute).Unix(),
	}
}

func TestJWTIdentityVerifierValidatesGoogleRS256AndMultiAudience(t *testing.T) {
	private, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(1_800_000_000, 0)
	claims := validClaims(now)
	claims.Audience = json.RawMessage(`["marketplace-client","secondary-client"]`)
	claims.AuthorizedParty = "marketplace-client"
	token := signIdentityJWT(t, "RS256", "google-key", private, claims)

	verifier := JWTIdentityVerifier{
		Policy: IdentityTokenPolicy{
			Provider: ProviderGoogle, Issuer: claims.Issuer, Audience: "marketplace-client",
			AllowedAlgorithms: []string{"RS256"},
		},
		Keys: staticSigningKey{SigningKey{Algorithm: "RS256", RSA: &private.PublicKey}},
		Now: func() time.Time { return now },
	}
	got, err := verifier.Verify(token)
	if err != nil {
		t.Fatal(err)
	}
	if got.Subject != "provider-subject" || got.Nonce != "one-time-state" || got.Provider != ProviderGoogle {
		t.Fatalf("unexpected verified identity: %#v", got)
	}
}

func TestJWTIdentityVerifierValidatesAppleES256(t *testing.T) {
	private, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	claims := validClaims(now)
	claims.Issuer = "https://appleid.apple.com"
	token := signIdentityJWT(t, "ES256", "apple-key", private, claims)

	verifier := JWTIdentityVerifier{
		Policy: IdentityTokenPolicy{
			Provider: ProviderApple, Issuer: claims.Issuer, Audience: "marketplace-client",
			AllowedAlgorithms: []string{"ES256"},
		},
		Keys: staticSigningKey{SigningKey{Algorithm: "ES256", ECDSA: &private.PublicKey}},
		Now: func() time.Time { return now },
	}
	got, err := verifier.Verify(token)
	if err != nil {
		t.Fatal(err)
	}
	if got.Provider != ProviderApple || got.Subject != claims.Subject {
		t.Fatalf("unexpected Apple identity: %#v", got)
	}
}

func TestJWTIdentityVerifierRejectsInvalidClaims(t *testing.T) {
	private, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	base := validClaims(now)
	tests := []struct {
		name   string
		mutate func(*jwtClaims)
	}{
		{name: "issuer mismatch", mutate: func(c *jwtClaims) { c.Issuer = "wrong-issuer" }},
		{name: "audience mismatch", mutate: func(c *jwtClaims) { c.Audience = json.RawMessage(`"other-client"`) }},
		{name: "single audience wrong authorized party", mutate: func(c *jwtClaims) { c.AuthorizedParty = "other-client" }},
		{name: "multiple audiences without authorized party", mutate: func(c *jwtClaims) { c.Audience = json.RawMessage(`["marketplace-client","other-client"]`) }},
		{name: "expired", mutate: func(c *jwtClaims) { c.ExpiresAt = now.Unix() }},
		{name: "issued in future", mutate: func(c *jwtClaims) { c.IssuedAt = now.Add(time.Minute).Unix() }},
		{name: "not yet valid", mutate: func(c *jwtClaims) { c.NotBefore = now.Add(time.Minute).Unix() }},
		{name: "empty subject", mutate: func(c *jwtClaims) { c.Subject = "  " }},
		{name: "empty nonce", mutate: func(c *jwtClaims) { c.Nonce = " " }},
	}
	verifier := JWTIdentityVerifier{
		Policy: IdentityTokenPolicy{
			Provider: ProviderGoogle, Issuer: base.Issuer, Audience: "marketplace-client",
			AllowedAlgorithms: []string{"RS256"},
		},
		Keys: staticSigningKey{SigningKey{Algorithm: "RS256", RSA: &private.PublicKey}},
		Now: func() time.Time { return now },
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			claims := base
			tt.mutate(&claims)
			token := signIdentityJWT(t, "RS256", "google-key", private, claims)
			if _, err := verifier.Verify(token); !errors.Is(err, ErrInvalidIdentityToken) {
				t.Fatalf("invalid claims accepted: %v", err)
			}
		})
	}
}

func TestJWTIdentityVerifierRejectsTamperingAndDisallowedAlgorithms(t *testing.T) {
	private, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	claims := validClaims(now)
	token := signIdentityJWT(t, "RS256", "key-1", private, claims)
	verifier := JWTIdentityVerifier{
		Policy: IdentityTokenPolicy{
			Provider: ProviderGoogle, Issuer: claims.Issuer, Audience: "marketplace-client",
			AllowedAlgorithms: []string{"ES256"},
		},
		Keys: staticSigningKey{SigningKey{Algorithm: "RS256", RSA: &private.PublicKey}},
		Now: func() time.Time { return now },
	}
	if _, err := verifier.Verify(token); !errors.Is(err, ErrInvalidIdentityToken) {
		t.Fatalf("disallowed algorithm accepted: %v", err)
	}

	verifier.Policy.AllowedAlgorithms = []string{"RS256"}
	if _, err := verifier.Verify(token + "x"); !errors.Is(err, ErrInvalidIdentityToken) {
		t.Fatalf("tampered token accepted: %v", err)
	}
}

func TestJWTIdentityVerifierFailsClosedWhenProviderKeysAreUnavailable(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	claims := validClaims(now)
	token := signIdentityJWT(t, "RS256", "rotated-key", mustRSAKey(t), claims)
	verifier := JWTIdentityVerifier{
		Policy: IdentityTokenPolicy{
			Provider: ProviderGoogle, Issuer: claims.Issuer, Audience: "marketplace-client",
			AllowedAlgorithms: []string{"RS256"},
		},
		Keys: unavailableSigningKey{},
		Now: func() time.Time { return now },
	}
	if _, err := verifier.Verify(token); !errors.Is(err, ErrIdentityKeyUnavailable) {
		t.Fatalf("provider key outage did not fail closed: %v", err)
	}
}

func TestJWTIdentityVerifierSupportsConfiguredEmailProofWithoutOIDCFlow(t *testing.T) {
	private, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	claims := validClaims(now)
	claims.Issuer = "https://email.example.test"
	claims.Subject = "opaque-email-proof-subject"
	token := signIdentityJWT(t, "RS256", "email-proof-key", private, claims)

	verifier := JWTIdentityVerifier{
		Policy: IdentityTokenPolicy{
			Provider: ProviderEmail, Issuer: claims.Issuer, Audience: "marketplace-client",
			AllowedAlgorithms: []string{"RS256"},
		},
		Keys: staticSigningKey{SigningKey{Algorithm: "RS256", RSA: &private.PublicKey}},
		Now: func() time.Time { return now },
	}
	got, err := verifier.Verify(token)
	if err != nil {
		t.Fatal(err)
	}
	if got.Provider != ProviderEmail || got.Subject != claims.Subject {
		t.Fatalf("unexpected email proof identity: %#v", got)
	}
}

func mustRSAKey(t *testing.T) *rsa.PrivateKey {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	return key
}
