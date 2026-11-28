package adapters

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
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

type jwtHeader struct {
	Algorithm string `json:"alg"`
	KeyID string `json:"kid"`
}

type jwtClaims struct {
	Issuer string `json:"iss"`
	Audience json.RawMessage `json:"aud"`
	Subject string `json:"sub"`
	ExpiresAt int64 `json:"exp"`
	IssuedAt int64 `json:"iat"`
	NotBefore int64 `json:"nbf,omitempty"`
	AuthorizedParty string `json:"azp,omitempty"`
	Nonce string `json:"nonce"`
}

// Verify validates a configured provider proof. Email providers use the same
// cryptographic JWT envelope only when explicitly configured; this does not
// interpret email verification as an OpenID Connect identity flow.
func (v JWTIdentityVerifier) Verify(token string) (VerifiedExternalIdentity, error) {
	if v.Keys == nil ||
		(v.Policy.Provider != ProviderApple && v.Policy.Provider != ProviderGoogle && v.Policy.Provider != ProviderEmail) ||
		strings.TrimSpace(v.Policy.Issuer) == "" ||
		strings.TrimSpace(v.Policy.Audience) == "" ||
		strings.TrimSpace(token) != token ||
		len(token) == 0 || len(token) > 16*1024 {
		return VerifiedExternalIdentity{}, ErrInvalidIdentityToken
	}

	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return VerifiedExternalIdentity{}, ErrInvalidIdentityToken
	}
	decode := func(segment string) ([]byte, error) {
		return base64.RawURLEncoding.DecodeString(segment)
	}

	headerBytes, err := decode(parts[0])
	if err != nil {
		return VerifiedExternalIdentity{}, ErrInvalidIdentityToken
	}
	var header jwtHeader
	if json.Unmarshal(headerBytes, &header) != nil ||
		strings.TrimSpace(header.KeyID) == "" || header.KeyID != strings.TrimSpace(header.KeyID) ||
		len(header.KeyID) > 256 || !v.allowed(header.Algorithm) {
		return VerifiedExternalIdentity{}, ErrInvalidIdentityToken
	}

	key, err := v.Keys.ResolveSigningKey(v.Policy.Provider, header.KeyID)
	if err != nil {
		return VerifiedExternalIdentity{}, ErrIdentityKeyUnavailable
	}
	signature, err := decode(parts[2])
	if err != nil {
		return VerifiedExternalIdentity{}, ErrInvalidIdentityToken
	}
	digest := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if !verifyJWTSignature(header.Algorithm, key, digest[:], signature) {
		return VerifiedExternalIdentity{}, ErrInvalidIdentityToken
	}

	claimBytes, err := decode(parts[1])
	if err != nil {
		return VerifiedExternalIdentity{}, ErrInvalidIdentityToken
	}
	var claims jwtClaims
	if json.Unmarshal(claimBytes, &claims) != nil ||
		claims.Issuer != v.Policy.Issuer ||
		strings.TrimSpace(claims.Subject) == "" || len(claims.Subject) > 255 ||
		strings.TrimSpace(claims.Nonce) == "" || len(claims.Nonce) > 256 {
		return VerifiedExternalIdentity{}, ErrInvalidIdentityToken
	}

	if !v.validAudience(claims.Audience, claims.AuthorizedParty) {
		return VerifiedExternalIdentity{}, ErrInvalidIdentityToken
	}

	now := time.Now()
	if v.Now != nil {
		now = v.Now()
	}
	issued := time.Unix(claims.IssuedAt, 0)
	expires := time.Unix(claims.ExpiresAt, 0)
	if claims.IssuedAt <= 0 || claims.ExpiresAt <= claims.IssuedAt ||
		!now.Before(expires) || issued.After(now) ||
		now.Before(time.Unix(claims.NotBefore, 0)) {
		return VerifiedExternalIdentity{}, ErrInvalidIdentityToken
	}

	return VerifiedExternalIdentity{
		Provider: v.Policy.Provider,
		Subject: claims.Subject,
		Nonce: claims.Nonce,
		IssuedAt: issued,
		ExpiresAt: expires,
	}, nil
}

func (v JWTIdentityVerifier) validAudience(raw json.RawMessage, authorizedParty string) bool {
	if len(raw) == 0 {
		return false
	}

	audienceCount := 0
	matched := false
	if raw[0] == '"' {
		var audience string
		if json.Unmarshal(raw, &audience) != nil || audience == "" {
			return false
		}
		audienceCount = 1
		matched = audience == v.Policy.Audience
	} else {
		var audiences []string
		if json.Unmarshal(raw, &audiences) != nil || len(audiences) == 0 {
			return false
		}
		audienceCount = len(audiences)
		for _, audience := range audiences {
			if audience == "" {
				return false
			}
			if audience == v.Policy.Audience {
				matched = true
			}
		}
	}

	if !matched {
		return false
	}
	if authorizedParty != "" && authorizedParty != v.Policy.Audience {
		return false
	}
	return audienceCount <= 1 || authorizedParty == v.Policy.Audience
}

func (v JWTIdentityVerifier) allowed(algorithm string) bool {
	for _, candidate := range v.Policy.AllowedAlgorithms {
		if candidate == algorithm {
			return algorithm == "RS256" || algorithm == "ES256"
		}
	}
	return false
}

func verifyJWTSignature(algorithm string, key SigningKey, digest, signature []byte) bool {
	switch algorithm {
	case "RS256":
		return key.Algorithm == algorithm && key.RSA != nil &&
			key.RSA.N != nil && key.RSA.N.Sign() > 0 && key.RSA.N.BitLen() >= 2048 &&
			key.RSA.E >= 3 && key.RSA.E%2 == 1 &&
			rsa.VerifyPKCS1v15(key.RSA, crypto.SHA256, digest, signature) == nil
	case "ES256":
		if key.Algorithm != algorithm || key.ECDSA == nil ||
			key.ECDSA.Curve != elliptic.P256() ||
			key.ECDSA.X == nil || key.ECDSA.Y == nil ||
			!elliptic.P256().IsOnCurve(key.ECDSA.X, key.ECDSA.Y) ||
			len(signature) != 64 {
			return false
		}
		r := new(big.Int).SetBytes(signature[:32])
		s := new(big.Int).SetBytes(signature[32:])
		der, err := asn1.Marshal(struct{ R, S *big.Int }{r, s})
		if err != nil {
			return false
		}
		return ecdsa.VerifyASN1(key.ECDSA, digest, der)
	default:
		return false
	}
}
