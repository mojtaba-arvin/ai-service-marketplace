package marketplace

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

var (
	ErrSessionInvalid = errors.New("session is invalid or expired")
	ErrSessionReplay  = errors.New("refresh token replay detected; session family revoked")
)

const sessionPepperMinBytes = 32

type SessionTokens struct {
	SessionID        string    `json:"session_id"`
	AccessToken      string    `json:"access_token"`
	RefreshToken     string    `json:"refresh_token"`
	AccessExpiresAt  time.Time `json:"access_expires_at"`
	RefreshExpiresAt time.Time `json:"refresh_expires_at"`
}

type SessionPrincipal struct {
	SessionID string
	IdentityID string
	Phone     string
}

func sessionToken() (string, error) {
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil { return "", fmt.Errorf("generate session token: %w", err) }
	return base64.RawURLEncoding.EncodeToString(raw[:]), nil
}

func sessionDigest(token string, pepper []byte) []byte {
	mac := hmac.New(sha256.New, pepper)
	_, _ = mac.Write([]byte(token))
	return mac.Sum(nil)
}

func validateSessionPolicy(pepper []byte, accessTTL, refreshTTL time.Duration) error {
	if len(pepper) < sessionPepperMinBytes { return errors.New("session pepper must contain at least 32 bytes") }
	if accessTTL < time.Minute || accessTTL > time.Hour { return errors.New("access token lifetime must be between one minute and one hour") }
	if refreshTTL < time.Hour || refreshTTL > 90*24*time.Hour || refreshTTL <= accessTTL {
		return errors.New("refresh token lifetime must be greater than access lifetime and at most 90 days")
	}
	return nil
}

func (s *Store) IssueSession(ctx context.Context, verified VerifiedIdentity, pepper []byte, accessTTL, refreshTTL time.Duration) (SessionTokens, error) {
	if err := validateSessionPolicy(pepper, accessTTL, refreshTTL); err != nil { return SessionTokens{}, err }
	if verified.phone == "" { return SessionTokens{}, ErrSessionInvalid }
	normalized, err := NormalizeE164Phone(verified.phone)
	if err != nil { return SessionTokens{}, err }
	var identityID string
	if err = s.DB.QueryRow(ctx, `SELECT id FROM phone_identities WHERE phone_e164=$1`, normalized).Scan(&identityID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) { return SessionTokens{}, ErrSessionInvalid }
		return SessionTokens{}, err
	}
	sessionID, err := newID()
	if err != nil { return SessionTokens{}, err }
	familyID, err := newID()
	if err != nil { return SessionTokens{}, err }
	access, err := sessionToken()
	if err != nil { return SessionTokens{}, err }
	refresh, err := sessionToken()
	if err != nil { return SessionTokens{}, err }
	now := time.Now().UTC()
	tokens := SessionTokens{SessionID: sessionID, AccessToken: access, RefreshToken: refresh,
		AccessExpiresAt: now.Add(accessTTL), RefreshExpiresAt: now.Add(refreshTTL)}
	_, err = s.DB.Exec(ctx, `
		INSERT INTO auth_sessions(id,family_id,phone_identity_id,device_id,access_token_hash,refresh_token_hash,access_expires_at,refresh_expires_at)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8)
	`, sessionID, familyID, identityID, sessionDigest(access, pepper), sessionDigest(refresh, pepper), tokens.AccessExpiresAt, tokens.RefreshExpiresAt)
	if err != nil { return SessionTokens{}, fmt.Errorf("persist session: %w", err) }
	return tokens, nil
}

func (s *Store) AuthenticateAccessToken(ctx context.Context, token string, pepper []byte) (SessionPrincipal, error) {
	if len(pepper) < sessionPepperMinBytes || token == "" { return SessionPrincipal{}, ErrSessionInvalid }
	var principal SessionPrincipal
	err := s.DB.QueryRow(ctx, `
		SELECT s.id, p.id, p.phone_e164
		FROM auth_sessions s JOIN phone_identities p ON p.id=s.phone_identity_id
		LEFT JOIN auth_devices d ON d.id=s.device_id
		WHERE s.access_token_hash=$1 AND s.revoked_at IS NULL AND s.access_expires_at > now()
		  AND (s.device_id IS NULL OR d.revoked_at IS NULL)
	`, sessionDigest(token, pepper)).Scan(&principal.SessionID, &principal.IdentityID, &principal.Phone)
	if errors.Is(err, pgx.ErrNoRows) { return SessionPrincipal{}, ErrSessionInvalid }
	if err != nil { return SessionPrincipal{}, err }
	return principal, nil
}

func (s *Store) RotateSession(ctx context.Context, refreshToken string, pepper []byte, accessTTL, refreshTTL time.Duration) (SessionTokens, error) {
	if err := validateSessionPolicy(pepper, accessTTL, refreshTTL); err != nil { return SessionTokens{}, err }
	if refreshToken == "" { return SessionTokens{}, ErrSessionInvalid }
	tx, err := s.DB.BeginTx(ctx, pgx.TxOptions{})
	if err != nil { return SessionTokens{}, err }
	defer tx.Rollback(ctx)

	var oldID, familyID, identityID string
	var deviceID *string
	var refreshExpiresAt time.Time
	var revoked, replaced bool
	err = tx.QueryRow(ctx, `
		SELECT id,family_id,phone_identity_id,device_id,refresh_expires_at,
		       revoked_at IS NOT NULL,replaced_by IS NOT NULL
		FROM auth_sessions WHERE refresh_token_hash=$1 FOR UPDATE
	`, sessionDigest(refreshToken, pepper)).Scan(&oldID, &familyID, &identityID, &deviceID, &refreshExpiresAt, &revoked, &replaced)
	if errors.Is(err, pgx.ErrNoRows) { return SessionTokens{}, ErrSessionInvalid }
	if err != nil { return SessionTokens{}, err }
	if revoked || replaced {
		if _, err = tx.Exec(ctx, `UPDATE auth_sessions SET revoked_at=COALESCE(revoked_at,now()) WHERE family_id=$1`, familyID); err != nil { return SessionTokens{}, err }
		if err = tx.Commit(ctx); err != nil { return SessionTokens{}, err }
		return SessionTokens{}, ErrSessionReplay
	}
	if !time.Now().Before(refreshExpiresAt) {
		_, _ = tx.Exec(ctx, `UPDATE auth_sessions SET revoked_at=now() WHERE id=$1`, oldID)
		if err = tx.Commit(ctx); err != nil { return SessionTokens{}, err }
		return SessionTokens{}, ErrSessionInvalid
	}

	newSessionID, err := newID()
	if err != nil { return SessionTokens{}, err }
	access, err := sessionToken()
	if err != nil { return SessionTokens{}, err }
	refresh, err := sessionToken()
	if err != nil { return SessionTokens{}, err }
	now := time.Now().UTC()
	tokens := SessionTokens{SessionID: newSessionID, AccessToken: access, RefreshToken: refresh,
		AccessExpiresAt: now.Add(accessTTL), RefreshExpiresAt: now.Add(refreshTTL)}
	_, err = tx.Exec(ctx, `
		INSERT INTO auth_sessions(id,family_id,phone_identity_id,device_id,access_token_hash,refresh_token_hash,access_expires_at,refresh_expires_at)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8)
	`, newSessionID, familyID, identityID, deviceID, sessionDigest(access, pepper), sessionDigest(refresh, pepper), tokens.AccessExpiresAt, tokens.RefreshExpiresAt)
	if err != nil { return SessionTokens{}, fmt.Errorf("persist rotated session: %w", err) }
	if _, err = tx.Exec(ctx, `UPDATE auth_sessions SET revoked_at=now(),replaced_by=$2 WHERE id=$1`, oldID, newSessionID); err != nil { return SessionTokens{}, err }
	if err = tx.Commit(ctx); err != nil { return SessionTokens{}, err }
	return tokens, nil
}

func (s *Store) RevokeSession(ctx context.Context, accessToken string, pepper []byte) error {
	if len(pepper) < sessionPepperMinBytes || accessToken == "" { return ErrSessionInvalid }
	tag, err := s.DB.Exec(ctx, `UPDATE auth_sessions SET revoked_at=COALESCE(revoked_at,now()) WHERE access_token_hash=$1`, sessionDigest(accessToken, pepper))
	if err != nil { return err }
	if tag.RowsAffected() != 1 { return ErrSessionInvalid }
	return nil
}
