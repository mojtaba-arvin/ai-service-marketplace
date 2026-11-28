package marketplace

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

var (
	ErrOperatorGrantNotFound = errors.New("operator permission grant not found")
	ErrOperatorGrantInput = errors.New("invalid operator permission grant input")
	ErrOperatorGrantExists = errors.New("operator permission grant already exists")
)

type OperatorActionGrant struct {
	ID           string     `json:"id"`
	IdentityID   string     `json:"identity_id"`
	TenantID     string     `json:"tenant_id"`
	Action       Action     `json:"action"`
	GrantedByID  *string    `json:"granted_by_identity_id,omitempty"`
	GrantReason  string     `json:"grant_reason"`
	GrantedAt    time.Time  `json:"granted_at"`
	RevokedAt    *time.Time `json:"revoked_at,omitempty"`
	RevokedByID  *string    `json:"revoked_by_identity_id,omitempty"`
	RevokeReason string     `json:"revoke_reason,omitempty"`
}

var operatorGrantableActions = map[Action]struct{}{
	ActionOrderRead:                  {},
	ActionOrderAssign:                {},
	ActionProviderVerify:             {},
	ActionCaseRead:                   {},
	ActionIncidentReview:             {},
	ActionMarketConfigure:            {},
	ActionCatalogManage:              {},
	ActionPolicyManage:               {},
	ActionAnalyticsRead:              {},
	ActionOperatorPermissionsManage:  {},
	ActionFinanceCaseManage:           {},
}

func validOperatorGrantAction(action Action) bool {
	_, ok := operatorGrantableActions[action]
	return ok
}

// authorizeOperatorTx is the authoritative operator permission check. Callers
// must use the same transaction for this check and the protected database work.
// FOR SHARE serializes the protected operation against a concurrent revocation
// (which takes FOR UPDATE on the same grant row).
func authorizeOperatorTx(ctx context.Context, tx pgx.Tx, actor Actor, resource Resource, action Action) error {
	if actor.Role != RoleOperator || actor.SubjectID == "" || actor.TenantID == "" ||
		resource.TenantID == "" || actor.TenantID != resource.TenantID ||
		!validOperatorGrantAction(action) {
		return ErrForbidden
	}

	var membershipID string
	err := tx.QueryRow(ctx, `
		SELECT id
		FROM identity_memberships
		WHERE identity_id=$1 AND tenant_id=$2 AND role='operator'
		FOR SHARE
	`, actor.SubjectID, actor.TenantID).Scan(&membershipID)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrForbidden
	}
	if err != nil {
		return err
	}

	var grantID string
	err = tx.QueryRow(ctx, `
		SELECT id
		FROM operator_action_grants
		WHERE identity_id=$1 AND tenant_id=$2 AND action=$3 AND revoked_at IS NULL
		FOR SHARE
	`, actor.SubjectID, actor.TenantID, string(action)).Scan(&grantID)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrForbidden
	}
	return err
}

// GrantOperatorAction grants one allowlisted action to another operator in the
// same tenant. Permission-management authority is itself an explicit grant;
// operators cannot grant themselves additional authority.
func (s *Store) GrantOperatorAction(ctx context.Context, actor Actor, tenantID, targetIdentityID string, action Action, reason string) (OperatorActionGrant, error) {
	tenantID = strings.TrimSpace(tenantID)
	targetIdentityID = strings.TrimSpace(targetIdentityID)
	reason = strings.TrimSpace(reason)
	if tenantID == "" || targetIdentityID == "" || len(targetIdentityID) > 200 || !validOperatorGrantAction(action) ||
		reason == "" || len(reason) > 500 {
		return OperatorActionGrant{}, ErrOperatorGrantInput
	}
	if targetIdentityID == actor.SubjectID {
		return OperatorActionGrant{}, ErrForbidden
	}

	tx, err := s.DB.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return OperatorActionGrant{}, err
	}
	defer tx.Rollback(ctx)

	if err = authorizeOperatorTx(ctx, tx, actor, Resource{TenantID: tenantID}, ActionOperatorPermissionsManage); err != nil {
		return OperatorActionGrant{}, err
	}

	var targetMembershipID string
	err = tx.QueryRow(ctx, `
		SELECT id FROM identity_memberships
		WHERE identity_id=$1 AND tenant_id=$2 AND role='operator'
		FOR SHARE
	`, targetIdentityID, tenantID).Scan(&targetMembershipID)
	if errors.Is(err, pgx.ErrNoRows) {
		return OperatorActionGrant{}, ErrForbidden
	}
	if err != nil {
		return OperatorActionGrant{}, err
	}

	id, err := newID()
	if err != nil {
		return OperatorActionGrant{}, err
	}
	var result OperatorActionGrant
	err = tx.QueryRow(ctx, `
		INSERT INTO operator_action_grants
			(id,identity_id,tenant_id,action,granted_by_kind,granted_by_identity_id,grant_reason)
		VALUES($1,$2,$3,$4,'operator',$5,$6)
		RETURNING id,identity_id,tenant_id,action,granted_by_identity_id,grant_reason,granted_at
	`, id, targetIdentityID, tenantID, string(action), actor.SubjectID, reason).Scan(
		&result.ID, &result.IdentityID, &result.TenantID, &result.Action,
		&result.GrantedByID, &result.GrantReason, &result.GrantedAt,
	)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return OperatorActionGrant{}, ErrOperatorGrantExists
		}
		return OperatorActionGrant{}, err
	}
	after, err := json.Marshal(map[string]any{
		"grant_id": result.ID, "active": true, "granted_at": result.GrantedAt.UTC(),
	})
	if err != nil {
		return OperatorActionGrant{}, err
	}
	if _, err = tx.Exec(ctx, `
		INSERT INTO operator_permission_events
			(grant_id,tenant_id,identity_id,action,event_type,actor_kind,actor_identity_id,
			 reason,before_state,after_state)
		VALUES($1,$2,$3,$4,'granted','operator',$5,$6,'{}'::jsonb,$7::jsonb)
	`, result.ID, tenantID, targetIdentityID, string(action), actor.SubjectID, reason, after); err != nil {
		return OperatorActionGrant{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return OperatorActionGrant{}, err
	}
	return result, nil
}

// RevokeOperatorAction is one-way and cannot be used by an operator to revoke
// their own permission-management grant. FOR UPDATE waits for all in-flight
// protected operations that hold FOR SHARE on the grant row.
func (s *Store) RevokeOperatorAction(ctx context.Context, actor Actor, tenantID, grantID, reason string) (OperatorActionGrant, error) {
	tenantID = strings.TrimSpace(tenantID)
	grantID = strings.TrimSpace(grantID)
	reason = strings.TrimSpace(reason)
	if tenantID == "" || grantID == "" || reason == "" || len(reason) > 500 {
		return OperatorActionGrant{}, ErrOperatorGrantInput
	}

	tx, err := s.DB.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return OperatorActionGrant{}, err
	}
	defer tx.Rollback(ctx)

	if err = authorizeOperatorTx(ctx, tx, actor, Resource{TenantID: tenantID}, ActionOperatorPermissionsManage); err != nil {
		return OperatorActionGrant{}, err
	}

	var result OperatorActionGrant
	err = tx.QueryRow(ctx, `
		SELECT id,identity_id,tenant_id,action,granted_by_identity_id,grant_reason,granted_at
		FROM operator_action_grants
		WHERE id=$1 AND tenant_id=$2 AND revoked_at IS NULL
		FOR UPDATE
	`, grantID, tenantID).Scan(
		&result.ID, &result.IdentityID, &result.TenantID, &result.Action,
		&result.GrantedByID, &result.GrantReason, &result.GrantedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return OperatorActionGrant{}, ErrOperatorGrantNotFound
	}
	if err != nil {
		return OperatorActionGrant{}, err
	}
	if result.IdentityID == actor.SubjectID {
		return OperatorActionGrant{}, ErrForbidden
	}

	err = tx.QueryRow(ctx, `
		UPDATE operator_action_grants
		SET revoked_at=now(),revoked_by_identity_id=$2,revoke_reason=$3
		WHERE id=$1
		RETURNING revoked_at,revoked_by_identity_id,revoke_reason
	`, grantID, actor.SubjectID, reason).Scan(&result.RevokedAt, &result.RevokedByID, &result.RevokeReason)
	if err != nil {
		return OperatorActionGrant{}, err
	}
	before, err := json.Marshal(map[string]any{
		"grant_id": result.ID, "active": true, "granted_at": result.GrantedAt.UTC(),
	})
	if err != nil {
		return OperatorActionGrant{}, err
	}
	after, err := json.Marshal(map[string]any{
		"grant_id": result.ID, "active": false, "revoked_at": result.RevokedAt.UTC(),
	})
	if err != nil {
		return OperatorActionGrant{}, err
	}
	if _, err = tx.Exec(ctx, `
		INSERT INTO operator_permission_events
			(grant_id,tenant_id,identity_id,action,event_type,actor_kind,actor_identity_id,
			 reason,before_state,after_state)
		VALUES($1,$2,$3,$4,'revoked','operator',$5,$6,$7::jsonb,$8::jsonb)
	`, result.ID, tenantID, result.IdentityID, string(result.Action), actor.SubjectID, reason, before, after); err != nil {
		return OperatorActionGrant{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return OperatorActionGrant{}, err
	}
	return result, nil
}


// ListOperatorActionGrants returns the tenant's permission history to an
// explicitly authorized permission manager. It includes revoked records so
// operators can audit why access was denied and who changed it.
func (s *Store) ListOperatorActionGrants(ctx context.Context, actor Actor) ([]OperatorActionGrant, error) {
	tx, err := s.DB.BeginTx(ctx, pgx.TxOptions{})
	if err != nil { return nil, err }
	defer tx.Rollback(ctx)
	if err = authorizeOperatorTx(ctx, tx, actor, Resource{TenantID: actor.TenantID}, ActionOperatorPermissionsManage); err != nil {
		return nil, err
	}
	rows, err := tx.Query(ctx, `
		SELECT id,identity_id,tenant_id,action,granted_by_identity_id,grant_reason,granted_at,
			revoked_at,revoked_by_identity_id,revoke_reason
		FROM operator_action_grants
		WHERE tenant_id=$1
		ORDER BY granted_at DESC,id DESC
		LIMIT 500
	`, actor.TenantID)
	if err != nil { return nil, err }
	grants := make([]OperatorActionGrant, 0)
	for rows.Next() {
		var grant OperatorActionGrant
		if err = rows.Scan(&grant.ID,&grant.IdentityID,&grant.TenantID,&grant.Action,&grant.GrantedByID,
			&grant.GrantReason,&grant.GrantedAt,&grant.RevokedAt,&grant.RevokedByID,&grant.RevokeReason); err != nil {
			rows.Close()
			return nil, err
		}
		grants = append(grants, grant)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()
	if err = tx.Commit(ctx); err != nil { return nil, err }
	return grants, nil
}
