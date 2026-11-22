package marketplace

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/mojtaba-arvin/ai-service-marketplace/apps/api/internal/outbox"
)

var (
	ErrMembershipForbidden = errors.New("organization membership operation forbidden")
	ErrMembershipConflict = errors.New("organization membership operation conflict")
	ErrMembershipNotFound = errors.New("organization membership not found")
)

type OrganizationMembershipInput struct {
	IdentityID string `json:"identity_id"`
	Role string `json:"role"`
}

type OrganizationMembershipResult struct {
	OrganizationID string `json:"organization_id"`
	IdentityID string `json:"identity_id"`
	Role string `json:"role"`
	Status string `json:"status"`
}

func validOrganizationRole(role string) bool {
	switch role {
	case "owner", "admin", "manager", "member", "viewer":
		return true
	default:
		return false
	}
}

func membershipManagerCanManage(actorRole, targetRole, nextRole string) bool {
	if actorRole == "owner" {
		return true
	}
	if actorRole == "admin" {
		return targetRole != "owner" && nextRole != "owner"
	}
	if actorRole == "manager" {
		return (targetRole == "member" || targetRole == "viewer") &&
			(nextRole == "member" || nextRole == "viewer")
	}
	return false
}

// InviteOrganizationIdentity invites an already-registered tenant identity.
// The organization row lock serializes membership changes and last-owner checks.
func (s *Store) InviteOrganizationIdentity(ctx context.Context, actorID, tenantID, organizationID string, in OrganizationMembershipInput) (OrganizationMembershipResult, error) {
	in.IdentityID = strings.TrimSpace(in.IdentityID)
	if actorID == "" || tenantID == "" || organizationID == "" || in.IdentityID == "" || !validOrganizationRole(in.Role) {
		return OrganizationMembershipResult{}, ErrOrganizationInput
	}
	tx, err := s.DB.BeginTx(ctx, pgx.TxOptions{})
	if err != nil { return OrganizationMembershipResult{}, err }
	defer tx.Rollback(ctx)

	var actorRole, actorIdentityRole string
	err = tx.QueryRow(ctx, `
		SELECT om.role, om.identity_role
		FROM organizations o
		JOIN organization_memberships om ON om.organization_id=o.id AND om.tenant_id=o.tenant_id
		WHERE o.id=$1 AND o.tenant_id=$2 AND o.status='active'
		  AND om.identity_id=$3 AND om.status='active'
		FOR UPDATE OF o
	`, organizationID, tenantID, actorID).Scan(&actorRole, &actorIdentityRole)
	if errors.Is(err, pgx.ErrNoRows) { return OrganizationMembershipResult{}, ErrMembershipForbidden }
	if err != nil { return OrganizationMembershipResult{}, err }
	if !membershipManagerCanManage(actorRole, in.Role, in.Role) { return OrganizationMembershipResult{}, ErrMembershipForbidden }
	if in.IdentityID == actorID { return OrganizationMembershipResult{}, ErrMembershipConflict }

	var targetIdentityRole string
	err = tx.QueryRow(ctx, `
		SELECT role FROM identity_memberships
		WHERE identity_id=$1 AND tenant_id=$2 AND role IN ('customer','professional')
		ORDER BY role LIMIT 1 FOR SHARE
	`, in.IdentityID, tenantID).Scan(&targetIdentityRole)
	if errors.Is(err, pgx.ErrNoRows) { return OrganizationMembershipResult{}, ErrMembershipNotFound }
	if err != nil { return OrganizationMembershipResult{}, err }

	var membershipID, status string
	err = tx.QueryRow(ctx, `
		SELECT id,status FROM organization_memberships
		WHERE organization_id=$1 AND tenant_id=$2 AND identity_id=$3 AND identity_role=$4
		FOR UPDATE
	`, organizationID, tenantID, in.IdentityID, targetIdentityRole).Scan(&membershipID, &status)
	if err == nil {
		if status != "revoked" { return OrganizationMembershipResult{}, ErrMembershipConflict }
		_, err = tx.Exec(ctx, `
			UPDATE organization_memberships SET role=$1,status='invited',
			  invited_by_identity_id=$2,invited_by_identity_role=$3,
			  revoked_at=NULL,updated_at=now()
			WHERE id=$4
		`, in.Role, actorID, actorIdentityRole, membershipID)
	} else if errors.Is(err, pgx.ErrNoRows) {
		membershipID, err = newID()
		if err == nil {
			_, err = tx.Exec(ctx, `
				INSERT INTO organization_memberships(id,organization_id,tenant_id,identity_id,identity_role,role,status,invited_by_identity_id,invited_by_identity_role)
				VALUES($1,$2,$3,$4,$5,$6,'invited',$7,$8)
			`, membershipID, organizationID, tenantID, in.IdentityID, targetIdentityRole, in.Role, actorID, actorIdentityRole)
		}
	}
	if err != nil {
		if isMembershipUniqueConflict(err) { return OrganizationMembershipResult{}, ErrMembershipConflict }
		return OrganizationMembershipResult{}, err
	}
	if err = appendOrganizationAudit(ctx, tx, organizationID, tenantID, actorID, actorIdentityRole, "organization.member.invited", map[string]string{"identity_id": in.IdentityID, "role": in.Role}); err != nil { return OrganizationMembershipResult{}, err }
	if err = appendMembershipEvent(ctx, tx, tenantID, organizationID, membershipID, "organization.member.invited", map[string]string{"identity_id": in.IdentityID, "role": in.Role}); err != nil { return OrganizationMembershipResult{}, err }
	if err = tx.Commit(ctx); err != nil { return OrganizationMembershipResult{}, err }
	return OrganizationMembershipResult{OrganizationID: organizationID, IdentityID: in.IdentityID, Role: in.Role, Status: "invited"}, nil
}

func (s *Store) AcceptOrganizationInvitation(ctx context.Context, identityID, tenantID, organizationID string) (OrganizationMembershipResult, error) {
	tx, err := s.DB.BeginTx(ctx, pgx.TxOptions{})
	if err != nil { return OrganizationMembershipResult{}, err }
	defer tx.Rollback(ctx)
	var identityRole, role string
	var membershipID string
	err = tx.QueryRow(ctx, `
		SELECT om.id,om.identity_role,om.role
		FROM organization_memberships om
		JOIN organizations o ON o.id=om.organization_id AND o.tenant_id=om.tenant_id
		WHERE om.organization_id=$1 AND om.tenant_id=$2 AND om.identity_id=$3
		  AND om.status='invited' AND o.status='active'
		FOR UPDATE OF o,om
	`, organizationID, tenantID, identityID).Scan(&membershipID, &identityRole, &role)
	if errors.Is(err, pgx.ErrNoRows) { return OrganizationMembershipResult{}, ErrMembershipNotFound }
	if err != nil { return OrganizationMembershipResult{}, err }
	_, err = tx.Exec(ctx, `UPDATE organization_memberships SET status='active',updated_at=now() WHERE id=$1`, membershipID)
	if err != nil { return OrganizationMembershipResult{}, err }
	if err = appendOrganizationAudit(ctx, tx, organizationID, tenantID, identityID, identityRole, "organization.member.accepted", map[string]string{"identity_id": identityID, "role": role}); err != nil { return OrganizationMembershipResult{}, err }
	if err = appendMembershipEvent(ctx, tx, tenantID, organizationID, membershipID, "organization.member.accepted", map[string]string{"identity_id": identityID, "role": role}); err != nil { return OrganizationMembershipResult{}, err }
	if err = tx.Commit(ctx); err != nil { return OrganizationMembershipResult{}, err }
	return OrganizationMembershipResult{OrganizationID: organizationID, IdentityID: identityID, Role: role, Status: "active"}, nil
}

func (s *Store) ChangeOrganizationMemberRole(ctx context.Context, actorID, tenantID, organizationID, targetID, nextRole string) (OrganizationMembershipResult, error) {
	if !validOrganizationRole(nextRole) { return OrganizationMembershipResult{}, ErrOrganizationInput }
	tx, err := s.DB.BeginTx(ctx, pgx.TxOptions{})
	if err != nil { return OrganizationMembershipResult{}, err }
	defer tx.Rollback(ctx)
	var actorRole, actorIdentityRole string
	err = tx.QueryRow(ctx, `
		SELECT om.role,om.identity_role FROM organizations o
		JOIN organization_memberships om ON om.organization_id=o.id AND om.tenant_id=o.tenant_id
		WHERE o.id=$1 AND o.tenant_id=$2 AND o.status='active' AND om.identity_id=$3 AND om.status='active'
		FOR UPDATE OF o
	`, organizationID, tenantID, actorID).Scan(&actorRole, &actorIdentityRole)
	if errors.Is(err, pgx.ErrNoRows) { return OrganizationMembershipResult{}, ErrMembershipForbidden }
	if err != nil { return OrganizationMembershipResult{}, err }
	var targetRole, targetIdentityRole, status, membershipID string
	err = tx.QueryRow(ctx, `
		SELECT id,role,identity_role,status FROM organization_memberships
		WHERE organization_id=$1 AND tenant_id=$2 AND identity_id=$3 FOR UPDATE
	`, organizationID, tenantID, targetID).Scan(&membershipID,&targetRole,&targetIdentityRole,&status)
	if errors.Is(err, pgx.ErrNoRows) { return OrganizationMembershipResult{}, ErrMembershipNotFound }
	if err != nil { return OrganizationMembershipResult{}, err }
	if status != "active" || !membershipManagerCanManage(actorRole,targetRole,nextRole) { return OrganizationMembershipResult{}, ErrMembershipForbidden }
	if targetRole == "owner" && nextRole != "owner" {
		var owners int
		if err = tx.QueryRow(ctx, `SELECT count(*) FROM organization_memberships WHERE organization_id=$1 AND tenant_id=$2 AND role='owner' AND status='active'`, organizationID,tenantID).Scan(&owners); err != nil { return OrganizationMembershipResult{}, err }
		if owners <= 1 { return OrganizationMembershipResult{}, ErrMembershipConflict }
	}
	_, err = tx.Exec(ctx, `UPDATE organization_memberships SET role=$1,updated_at=now() WHERE id=$2`, nextRole,membershipID)
	if err != nil { return OrganizationMembershipResult{}, err }
	if err = appendOrganizationAudit(ctx,tx,organizationID,tenantID,actorID,actorIdentityRole,"organization.member.role_changed",map[string]string{"identity_id":targetID,"from":targetRole,"to":nextRole}); err != nil { return OrganizationMembershipResult{}, err }
	if err = appendMembershipEvent(ctx,tx,tenantID,organizationID,membershipID,"organization.member.role_changed",map[string]string{"identity_id":targetID,"from":targetRole,"to":nextRole}); err != nil { return OrganizationMembershipResult{}, err }
	if err = tx.Commit(ctx); err != nil { return OrganizationMembershipResult{}, err }
	return OrganizationMembershipResult{OrganizationID:organizationID,IdentityID:targetID,Role:nextRole,Status:"active"},nil
}

func (s *Store) RevokeOrganizationMember(ctx context.Context, actorID, tenantID, organizationID, targetID string) error {
	tx, err := s.DB.BeginTx(ctx, pgx.TxOptions{})
	if err != nil { return err }
	defer tx.Rollback(ctx)
	var actorRole, actorIdentityRole string
	err = tx.QueryRow(ctx, `
		SELECT om.role,om.identity_role FROM organizations o
		JOIN organization_memberships om ON om.organization_id=o.id AND om.tenant_id=o.tenant_id
		WHERE o.id=$1 AND o.tenant_id=$2 AND o.status='active' AND om.identity_id=$3 AND om.status='active'
		FOR UPDATE OF o
	`,organizationID,tenantID,actorID).Scan(&actorRole,&actorIdentityRole)
	if errors.Is(err,pgx.ErrNoRows) { return ErrMembershipForbidden }
	if err!=nil { return err }
	var targetRole,targetIdentityRole,status,membershipID string
	err=tx.QueryRow(ctx,`SELECT id,role,identity_role,status FROM organization_memberships WHERE organization_id=$1 AND tenant_id=$2 AND identity_id=$3 FOR UPDATE`,organizationID,tenantID,targetID).Scan(&membershipID,&targetRole,&targetIdentityRole,&status)
	if errors.Is(err,pgx.ErrNoRows) { return ErrMembershipNotFound }
	if err!=nil { return err }
	if status!="active" || targetID==actorID || !membershipManagerCanManage(actorRole,targetRole,"") { return ErrMembershipForbidden }
	if targetRole=="owner" {
		if actorRole!="owner" { return ErrMembershipForbidden }
		var owners int
		if err=tx.QueryRow(ctx,`SELECT count(*) FROM organization_memberships WHERE organization_id=$1 AND tenant_id=$2 AND role='owner' AND status='active'`,organizationID,tenantID).Scan(&owners);err!=nil{return err}
		if owners<=1{return ErrMembershipConflict}
	}
	_,err=tx.Exec(ctx,`UPDATE organization_memberships SET status='revoked',revoked_at=now(),updated_at=now() WHERE id=$1`,membershipID)
	if err!=nil{return err}
	if err=appendOrganizationAudit(ctx,tx,organizationID,tenantID,actorID,actorIdentityRole,"organization.member.revoked",map[string]string{"identity_id":targetID,"role":targetRole});err!=nil{return err}
	if err=appendMembershipEvent(ctx,tx,tenantID,organizationID,membershipID,"organization.member.revoked",map[string]string{"identity_id":targetID,"role":targetRole});err!=nil{return err}
	return tx.Commit(ctx)
}

func appendMembershipEvent(ctx context.Context, tx pgx.Tx, tenantID, organizationID, membershipID, eventType string, data map[string]string) error {
	payload,err:=json.Marshal(data); if err!=nil{return err}
	eventID,err:=newID();if err!=nil{return err}
	return appendDomainEvent(ctx,tx,outbox.DomainEvent{ID:eventID,Type:eventType,Version:1,OccurredAt:time.Now().UTC(),TenantID:tenantID,Aggregate:outbox.EventAggregate{Type:"organization_membership",ID:membershipID},Data:payload})
}

func isMembershipUniqueConflict(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err,&pgErr) && pgErr.Code=="23505"
}

func (h *Handler) organizationMembershipRoutes(w http.ResponseWriter,r *http.Request,organizationID string) bool {
	principal,err:=h.principal(r)
	if err!=nil { writeError(w,http.StatusUnauthorized,"unauthorized");return true }
	tenantID:=h.organizationTenant(r)
	parts:=strings.Split(strings.Trim(r.URL.Path,"/"),"/")
	if len(parts)==5 && parts[3]=="memberships" && parts[4]=="invitations" && r.Method==http.MethodPost {
		var in OrganizationMembershipInput
		if err=decodeOrganizationInput(r,&in);err!=nil { writeError(w,http.StatusBadRequest,"invalid invitation payload");return true }
		item,e:=h.Store.InviteOrganizationIdentity(r.Context(),principal.IdentityID,tenantID,organizationID,in)
		writeMembershipResult(w,item,e)
		return true
	}
	if len(parts)==6 && parts[3]=="invitations" && parts[5]=="accept" && r.Method==http.MethodPost {
		item,e:=h.Store.AcceptOrganizationInvitation(r.Context(),principal.IdentityID,tenantID,organizationID)
		writeMembershipResult(w,item,e)
		return true
	}
	if len(parts)==6 && parts[3]=="memberships" && parts[5]=="role" && r.Method==http.MethodPatch {
		var in struct{ Role string `json:"role"` }
		if err=decodeOrganizationInput(r,&in);err!=nil { writeError(w,http.StatusBadRequest,"invalid role payload");return true }
		item,e:=h.Store.ChangeOrganizationMemberRole(r.Context(),principal.IdentityID,tenantID,organizationID,parts[4],in.Role)
		writeMembershipResult(w,item,e)
		return true
	}
	if len(parts)==5 && parts[3]=="memberships" && r.Method==http.MethodDelete {
		e:=h.Store.RevokeOrganizationMember(r.Context(),principal.IdentityID,tenantID,organizationID,parts[4])
		if e!=nil { writeMembershipError(w,e);return true }
		writeJSON(w,http.StatusOK,map[string]string{"status":"revoked"})
		return true
	}
	return false
}

func writeMembershipResult(w http.ResponseWriter,item OrganizationMembershipResult,err error) {
	if err!=nil { writeMembershipError(w,err);return }
	status:=http.StatusOK
	if item.Status=="invited" { status=http.StatusCreated }
	writeJSON(w,status,map[string]any{"membership":item})
}

func writeMembershipError(w http.ResponseWriter,err error) {
	switch {
	case errors.Is(err,ErrMembershipForbidden): writeError(w,http.StatusForbidden,"membership operation forbidden")
	case errors.Is(err,ErrMembershipNotFound): writeError(w,http.StatusNotFound,"membership not found")
	case errors.Is(err,ErrMembershipConflict): writeError(w,http.StatusConflict,"membership state conflict")
	case errors.Is(err,ErrOrganizationInput): writeError(w,http.StatusBadRequest,"invalid membership payload")
	default: writeError(w,http.StatusInternalServerError,"membership operation failed")
	}
}
