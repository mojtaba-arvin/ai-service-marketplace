package marketplace

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"fmt"\n\t"math/big"
	"time"

	"github.com/jackc/pgx/v5"
)

var (
	ErrOTPInvalidChallenge = errors.New("OTP challenge is invalid, expired, consumed, or exhausted")
	ErrOTPInvalidCode      = errors.New("OTP code is invalid")
	ErrOTPRateLimited      = errors.New("OTP issuance rate limit exceeded")
)

const (
	otpCodeLength       = 6
	otpPepperMinBytes   = 32
	otpMaxIssuancesHour = 5
)

// GenerateOTPCode uses the operating system CSPRNG and returns a zero-padded
// six-digit code. Callers must deliver it through an out-of-band sender.
func GenerateOTPCode() (string, error) {
	var raw [4]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", fmt.Errorf("generate OTP: %w", err)
	}
	value := uint32(raw[0])<<24 | uint32(raw[1])<<16 | uint32(raw[2])<<8 | uint32(raw[3])
	return fmt.Sprintf("%06d", value%1_000_000), nil
}

func otpDigest(challengeID, code string, pepper []byte) []byte {
	mac := hmac.New(sha256.New, pepper)
	_, _ = mac.Write([]byte(challengeID))
	_, _ = mac.Write([]byte{':'})
	_, _ = mac.Write([]byte(code))
	return mac.Sum(nil)
}

// CreateOTPChallenge stores only a keyed digest. The returned ID and plaintext
// code are intended for a delivery adapter; neither is persisted in plaintext.
func (s *Store) CreateOTPChallenge(ctx context.Context, phone, code string, pepper []byte, ttl time.Duration, maxAttempts int) (string, error) {
	normalized, err := NormalizeE164Phone(phone)
	if err != nil {
		return "", err
	}
	if len(pepper) < otpPepperMinBytes {
		return "", errors.New("OTP pepper must contain at least 32 bytes")
	}
	if len(code) != otpCodeLength {
		return "", errors.New("OTP code must contain exactly six digits")
	}
	for _, digit := range code {
		if digit < '0' || digit > '9' {
			return "", errors.New("OTP code must contain exactly six digits")
		}
	}
	if ttl < time.Minute || ttl > 15*time.Minute {
		return "", errors.New("OTP expiry must be between one and fifteen minutes")
	}
	if maxAttempts < 1 || maxAttempts > 10 {
		return "", errors.New("OTP max attempts must be between one and ten")
	}

	tx, err := s.DB.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx)

	identityID, err := newID()
	if err != nil {
		return "", err
	}
	var persistedIdentityID string
	err = tx.QueryRow(ctx, `
		INSERT INTO phone_identities(id, phone_e164) VALUES($1, $2)
		ON CONFLICT(phone_e164) DO UPDATE SET phone_e164 = EXCLUDED.phone_e164
		RETURNING id
	`, identityID, normalized).Scan(&persistedIdentityID)
	if err != nil {
		return "", fmt.Errorf("upsert phone identity: %w", err)
	}

	// Serialize issuance per identity before applying the rolling-window limit.
	var lockedID string
	if err = tx.QueryRow(ctx, `SELECT id FROM phone_identities WHERE id=$1 FOR UPDATE`, persistedIdentityID).Scan(&lockedID); err != nil {
		return "", err
	}
	var recent int
	if err = tx.QueryRow(ctx, `SELECT count(*) FROM otp_challenges WHERE phone_identity_id=$1 AND created_at > now() - interval '1 hour'`, persistedIdentityID).Scan(&recent); err != nil {
		return "", err
	}
	if recent >= otpMaxIssuancesHour {
		return "", ErrOTPRateLimited
	}

	if _, err = tx.Exec(ctx, `
		UPDATE otp_challenges SET invalidated_at=now()
		WHERE phone_identity_id=$1 AND consumed_at IS NULL AND invalidated_at IS NULL
	`, persistedIdentityID); err != nil {
		return "", err
	}

	challengeID, err := newID()
	if err != nil {
		return "", err
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO otp_challenges(id, phone_identity_id, code_hash, expires_at, max_attempts)
		VALUES($1, $2, $3, now() + ($4 * interval '1 second'), $5)
	`, challengeID, persistedIdentityID, otpDigest(challengeID, code, pepper), ttl.Seconds(), maxAttempts)
	if err != nil {
		return "", fmt.Errorf("persist OTP challenge: %w", err)
	}
	if err = tx.Commit(ctx); err != nil {
		return "", err
	}
	return challengeID, nil
}

// VerifyOTPChallenge consumes a challenge exactly once. Failed attempts are
// persisted under a row lock so concurrent guesses cannot exceed the limit.
func (s *Store) VerifyOTPChallenge(ctx context.Context, challengeID, code string, pepper []byte) (string, error) {
	if len(pepper) < otpPepperMinBytes {
		return "", errors.New("OTP pepper must contain at least 32 bytes")
	}
	if challengeID == "" || len(code) != otpCodeLength {
		return "", ErrOTPInvalidChallenge
	}
	tx, err := s.DB.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx)

	var phone, storedHash []byte
	var expiresAt time.Time
	var attempts, maxAttempts int
	var consumed, invalidated bool
	err = tx.QueryRow(ctx, `
		SELECT p.phone_e164, c.code_hash, c.expires_at, c.attempt_count, c.max_attempts,
		       c.consumed_at IS NOT NULL, c.invalidated_at IS NOT NULL
		FROM otp_challenges c JOIN phone_identities p ON p.id=c.phone_identity_id
		WHERE c.id=$1 FOR UPDATE OF c
	`, challengeID).Scan(&phone, &storedHash, &expiresAt, &attempts, &maxAttempts, &consumed, &invalidated)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrOTPInvalidChallenge
	}
	if err != nil {
		return "", err
	}
	if consumed || invalidated || !time.Now().Before(expiresAt) || attempts >= maxAttempts {
		return "", ErrOTPInvalidChallenge
	}

	expected := otpDigest(challengeID, code, pepper)
	if subtle.ConstantTimeCompare(storedHash, expected) != 1 {
		if _, err = tx.Exec(ctx, `UPDATE otp_challenges SET attempt_count=attempt_count+1 WHERE id=$1`, challengeID); err != nil {
			return "", err
		}
		if err = tx.Commit(ctx); err != nil {
			return "", err
		}
		return "", ErrOTPInvalidCode
	}
	if _, err = tx.Exec(ctx, `UPDATE otp_challenges SET attempt_count=attempt_count+1, consumed_at=now() WHERE id=$1`, challengeID); err != nil {
		return "", err
	}
	if err = tx.Commit(ctx); err != nil {
		return "", err
	}
	return string(phone), nil
}
