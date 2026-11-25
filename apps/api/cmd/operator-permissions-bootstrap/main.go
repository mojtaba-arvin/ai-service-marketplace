package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var allowedActions = map[string]struct{}{
	"order:read": {},
	"order:assign": {},
	"provider:verify": {},
	"case:read": {},
	"market:configure": {},
	"catalog:manage": {},
	"policy:manage": {},
	"analytics:read": {},
	"operator:permissions:manage": {},
}

func parseActions(raw string) ([]string, error) {
	parts := strings.Split(raw, ",")
	actions := make([]string, 0, len(parts))
	seen := make(map[string]struct{}, len(parts))
	for _, part := range parts {
		action := strings.TrimSpace(part)
		if action == "" {
			return nil, errors.New("BOOTSTRAP_OPERATOR_ACTIONS contains an empty action")
		}
		if _, ok := allowedActions[action]; !ok {
			return nil, fmt.Errorf("unsupported operator action %q", action)
		}
		if _, ok := seen[action]; ok {
			return nil, fmt.Errorf("duplicate operator action %q", action)
		}
		seen[action] = struct{}{}
		actions = append(actions, action)
	}
	if len(actions) == 0 {
		return nil, errors.New("at least one operator action is required")
	}
	sort.Strings(actions)
	return actions, nil
}

func newID() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw[:]), nil
}

func bootstrap(ctx context.Context, pool *pgxpool.Pool, tenantID, identityID string, actions []string, reason string) error {
	tenantID = strings.TrimSpace(tenantID)
	identityID = strings.TrimSpace(identityID)
	reason = strings.TrimSpace(reason)
	if tenantID == "" || len(tenantID) > 120 || identityID == "" || reason == "" || len(reason) > 500 || len(actions) == 0 {
		return errors.New("tenant, operator identity, non-empty actions and a 1..500 character reason are required")
	}
	manifest := tenantID + "\n" + identityID + "\n" + strings.Join(actions, "\n")
	sum := sha256.Sum256([]byte(manifest))
	manifestHash := hex.EncodeToString(sum[:])

	tx, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return fmt.Errorf("begin bootstrap transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	if _, err = tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1, 0))", "operator-permissions-bootstrap:"+tenantID); err != nil {
		return fmt.Errorf("lock tenant bootstrap: %w", err)
	}
	var membershipID string
	err = tx.QueryRow(ctx, `
		SELECT id FROM identity_memberships
		WHERE identity_id=$1 AND tenant_id=$2 AND role='operator'
		FOR SHARE
	`, identityID, tenantID).Scan(&membershipID)
	if errors.Is(err, pgx.ErrNoRows) {
		return errors.New("bootstrap identity must already have an operator membership in the target tenant")
	}
	if err != nil {
		return fmt.Errorf("verify operator membership: %w", err)
	}

	tag, err := tx.Exec(ctx, `
		INSERT INTO operator_permission_bootstraps
			(tenant_id,bootstrap_identity_id,manifest_sha256,grants_count,created_by)
		VALUES($1,$2,$3,$4,'system_bootstrap')
		ON CONFLICT(tenant_id) DO NOTHING
	`, tenantID, identityID, manifestHash, len(actions))
	if err != nil {
		return fmt.Errorf("record one-time bootstrap marker: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return errors.New("operator permissions have already been bootstrapped for this tenant; refusing replay")
	}

	for _, action := range actions {
		id, idErr := newID()
		if idErr != nil {
			return fmt.Errorf("generate grant id: %w", idErr)
		}
		var grantTime time.Time
		err = tx.QueryRow(ctx, `
			INSERT INTO operator_action_grants
				(id,identity_id,tenant_id,action,granted_by_kind,granted_by_identity_id,grant_reason)
			VALUES($1,$2,$3,$4,'system_bootstrap',NULL,$5)
			RETURNING granted_at
		`, id, identityID, tenantID, action, reason).Scan(&grantTime)
		if err != nil {
			return fmt.Errorf("insert %s grant: %w", action, err)
		}
		after := fmt.Sprintf(`{"grant_id":%q,"active":true,"granted_by":"system_bootstrap","granted_at":%q}`, id, grantTime.UTC().Format(time.RFC3339Nano))
		if _, err = tx.Exec(ctx, `
			INSERT INTO operator_permission_events
				(tenant_id,identity_id,action,event_type,actor_kind,actor_identity_id,reason,before_state,after_state)
			VALUES($1,$2,$3,'granted','system_bootstrap',NULL,$4,'{}'::jsonb,$5::jsonb)
		`, tenantID, identityID, action, reason, after); err != nil {
			return fmt.Errorf("append %s grant audit event: %w", action, err)
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit operator permission bootstrap: %w", err)
	}
	log.Printf("bootstrapped %d scoped operator actions for tenant %q; manifest sha256=%s", len(actions), tenantID, manifestHash)
	return nil
}

func run(ctx context.Context, lookup func(string) string) error {
	dsn := lookup("DATABASE_URL")
	tenantID := lookup("BOOTSTRAP_TENANT_ID")
	identityID := lookup("BOOTSTRAP_OPERATOR_IDENTITY_ID")
	reason := lookup("BOOTSTRAP_REASON")
	if dsn == "" {
		return errors.New("DATABASE_URL is required")
	}
	actions, err := parseActions(lookup("BOOTSTRAP_OPERATOR_ACTIONS"))
	if err != nil {
		return err
	}
	if tenantID == "" || identityID == "" || reason == "" {
		return errors.New("BOOTSTRAP_TENANT_ID, BOOTSTRAP_OPERATOR_IDENTITY_ID and BOOTSTRAP_REASON are required")
	}
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return fmt.Errorf("configure database pool: %w", err)
	}
	defer pool.Close()
	if err = pool.Ping(ctx); err != nil {
		return fmt.Errorf("connect to database: %w", err)
	}
	return bootstrap(ctx, pool, tenantID, identityID, actions, reason)
}

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := run(ctx, os.Getenv); err != nil {
		log.Fatal(err)
	}
}
