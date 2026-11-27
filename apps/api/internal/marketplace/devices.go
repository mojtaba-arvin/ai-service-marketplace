package marketplace

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

var ErrDeviceNotFound = errors.New("device not found")

type AuthDevice struct {
	ID         string     `json:"id"`
	Label      string     `json:"label"`
	CreatedAt  time.Time  `json:"created_at"`
	LastSeenAt time.Time  `json:"last_seen_at"`
	RevokedAt  *time.Time `json:"revoked_at,omitempty"`
}

func (s *Store) RegisterDevice(ctx context.Context, identityID, sessionID, label string) (AuthDevice, error) {
	label = strings.TrimSpace(label)
	if identityID == "" || sessionID == "" || label == "" || len(label) > 80 {
		return AuthDevice{}, errors.New("identity, active session and device label are required")
	}
	tx, err := s.DB.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return AuthDevice{}, err
	}
	defer tx.Rollback(ctx)

	var existingID *string
	err = tx.QueryRow(ctx, `
		SELECT device_id FROM auth_sessions
		WHERE id=$1 AND phone_identity_id=$2 AND revoked_at IS NULL AND refresh_expires_at>now()
		FOR UPDATE
	`, sessionID, identityID).Scan(&existingID)
	if errors.Is(err, pgx.ErrNoRows) {
		return AuthDevice{}, ErrSessionInvalid
	}
	if err != nil {
		return AuthDevice{}, err
	}

	if existingID != nil {
		var device AuthDevice
		err = tx.QueryRow(ctx, `
			UPDATE auth_devices SET label=$3,last_seen_at=now()
			WHERE id=$1 AND identity_id=$2 AND revoked_at IS NULL
			RETURNING id,label,created_at,last_seen_at,revoked_at
		`, *existingID, identityID, label).Scan(&device.ID, &device.Label, &device.CreatedAt, &device.LastSeenAt, &device.RevokedAt)
		if errors.Is(err, pgx.ErrNoRows) {
			return AuthDevice{}, ErrDeviceNotFound
		}
		if err != nil {
			return AuthDevice{}, err
		}
		if err = tx.Commit(ctx); err != nil {
			return AuthDevice{}, err
		}
		return device, nil
	}

	deviceID, err := newID()
	if err != nil {
		return AuthDevice{}, err
	}
	var device AuthDevice
	err = tx.QueryRow(ctx, `
		INSERT INTO auth_devices(id,identity_id,label)
		VALUES($1,$2,$3)
		RETURNING id,label,created_at,last_seen_at,revoked_at
	`, deviceID, identityID, label).Scan(&device.ID, &device.Label, &device.CreatedAt, &device.LastSeenAt, &device.RevokedAt)
	if err != nil {
		return AuthDevice{}, fmt.Errorf("register device: %w", err)
	}
	if _, err = tx.Exec(ctx, `UPDATE auth_sessions SET device_id=$2 WHERE id=$1`, sessionID, deviceID); err != nil {
		return AuthDevice{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return AuthDevice{}, err
	}
	return device, nil
}

func (s *Store) ListDevices(ctx context.Context, identityID string) ([]AuthDevice, error) {
	if strings.TrimSpace(identityID) == "" {
		return nil, ErrSessionInvalid
	}
	rows, err := s.DB.Query(ctx, `
		SELECT id,label,created_at,last_seen_at,revoked_at
		FROM auth_devices WHERE identity_id=$1
		ORDER BY created_at DESC,id
		LIMIT 100
	`, identityID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	devices := make([]AuthDevice, 0)
	for rows.Next() {
		var device AuthDevice
		if err = rows.Scan(&device.ID, &device.Label, &device.CreatedAt, &device.LastSeenAt, &device.RevokedAt); err != nil {
			return nil, err
		}
		devices = append(devices, device)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	return devices, nil
}

func (s *Store) RevokeDevice(ctx context.Context, identityID, deviceID string) error {
	if strings.TrimSpace(identityID) == "" || strings.TrimSpace(deviceID) == "" {
		return ErrDeviceNotFound
	}
	tx, err := s.DB.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var revokedAt *time.Time
	err = tx.QueryRow(ctx, `
		SELECT revoked_at FROM auth_devices
		WHERE id=$1 AND identity_id=$2 FOR UPDATE
	`, deviceID, identityID).Scan(&revokedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrDeviceNotFound
	}
	if err != nil {
		return err
	}
	if revokedAt == nil {
		if _, err = tx.Exec(ctx, `UPDATE auth_devices SET revoked_at=now() WHERE id=$1`, deviceID); err != nil {
			return err
		}
	}
	if _, err = tx.Exec(ctx, `
		UPDATE auth_sessions SET revoked_at=COALESCE(revoked_at,now())
		WHERE device_id=$1
	`, deviceID); err != nil {
		return err
	}
	if err = tx.Commit(ctx); err != nil {
		return err
	}
	return nil
}
