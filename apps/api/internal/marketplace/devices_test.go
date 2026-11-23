package marketplace

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestDeviceRegistrationListingAndRevocationPostgres(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" { t.Skip("TEST_DATABASE_URL is not configured") }
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil { t.Fatal(err) }
	defer pool.Close()
	for _, name := range []string{"002_identity.sql", "003_sessions.sql", "025_auth_devices.sql"} {
		raw, readErr := os.ReadFile(filepath.Join("..", "..", "migrations", name))
		if readErr != nil { t.Fatal(readErr) }
		if _, err = execMigrationScript(ctx, pool, raw); err != nil { t.Fatalf("apply %s: %v", name, err) }
	}
	pepper := []byte("device-test-session-pepper-material-at-least-32-bytes")
	identityID, err := newID()
	if err != nil { t.Fatal(err) }
	sessionID, err := newID()
	if err != nil { t.Fatal(err) }
	familyID, err := newID()
	if err != nil { t.Fatal(err) }
	phone := fmt.Sprintf("+1994%011d", time.Now().UnixNano()%100000000000)
	_, err = pool.Exec(ctx, `INSERT INTO phone_identities(id,phone_e164) VALUES($1,$2)`, identityID, phone)
	if err != nil { t.Fatal(err) }
	access, refresh := "device-access-"+sessionID, "device-refresh-"+sessionID
	_, err = pool.Exec(ctx, `
		INSERT INTO auth_sessions(id,family_id,phone_identity_id,access_token_hash,refresh_token_hash,access_expires_at,refresh_expires_at)
		VALUES($1,$2,$3,$4,$5,now()+interval '15 minutes',now()+interval '1 day')
	`, sessionID, familyID, identityID, sessionDigest(access, pepper), sessionDigest(refresh, pepper))
	if err != nil { t.Fatal(err) }
	store := &Store{DB: pool}
	device, err := store.RegisterDevice(ctx, identityID, sessionID, "Personal laptop")
	if err != nil { t.Fatal(err) }
	if device.ID == "" || device.Label != "Personal laptop" || device.RevokedAt != nil { t.Fatalf("unexpected device: %#v", device) }
	listed, err := store.ListDevices(ctx, identityID)
	if err != nil || len(listed) != 1 || listed[0].ID != device.ID { t.Fatalf("list devices: %#v %v", listed, err) }
	if _, err = store.RegisterDevice(ctx, identityID, sessionID, "Renamed laptop"); err != nil { t.Fatal(err) }
	listed, err = store.ListDevices(ctx, identityID)
	if err != nil || len(listed) != 1 || listed[0].Label != "Renamed laptop" { t.Fatalf("device update: %#v %v", listed, err) }
	if err = store.RevokeDevice(ctx, "another-identity", device.ID); !errors.Is(err, ErrDeviceNotFound) { t.Fatalf("cross-identity revoke should be hidden: %v", err) }
	rotated, err := store.RotateSession(ctx, refresh, pepper, 15*time.Minute, 24*time.Hour)
	if err != nil { t.Fatalf("rotate device-bound session: %v", err) }
	var carriedDeviceID string
	if err = pool.QueryRow(ctx, `SELECT device_id FROM auth_sessions WHERE id=$1`, rotated.SessionID).Scan(&carriedDeviceID); err != nil { t.Fatal(err) }
	if carriedDeviceID != device.ID { t.Fatalf("refresh rotation lost device binding: got %q want %q", carriedDeviceID, device.ID) }
	if _, err = store.AuthenticateAccessToken(ctx, rotated.AccessToken, pepper); err != nil { t.Fatalf("rotated session should authenticate before device revocation: %v", err) }
	if err = store.RevokeDevice(ctx, identityID, device.ID); err != nil { t.Fatal(err) }
	if _, err = store.AuthenticateAccessToken(ctx, rotated.AccessToken, pepper); !errors.Is(err, ErrSessionInvalid) { t.Fatalf("revoked device session still authenticates: %v", err) }
	if err = store.RevokeDevice(ctx, identityID, device.ID); err != nil { t.Fatalf("repeated revoke should be idempotent: %v", err) }
}
