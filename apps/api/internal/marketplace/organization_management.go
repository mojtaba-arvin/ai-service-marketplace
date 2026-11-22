package marketplace

import (
	"context"
	"encoding/json"
	"errors"
		"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/mojtaba-arvin/ai-service-marketplace/apps/api/internal/outbox"
)

var (
	ErrOrganizationConflict = errors.New("organization conflict")
	ErrOrganizationInvalid = errors.New("invalid organization request")
	ErrOrganizationForbidden = errors.New("organization operation forbidden")
)

var organizationSlugPattern = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

type OrganizationCreateInput struct {
	Name string `json:"name"`
	Slug string `json:"slug"`
}
type PropertyCreateInput struct {
	Code string `json:"property_code"`
	Name string `json:"name"`
	CountryCode string `json:"country_code"`
	RegionCode string `json:"region_code"`
	CityCode string `json:"city_code"`
	AreaCode string `json:"area_code"`
}
type OrganizationInvitationInput struct {
	IdentityID string `json:"identity_id"`
	IdentityRole string `json:"identity_role"`
	Role string `json:"role"`
}
type OrganizationMembershipSummary struct {
	ID string `json:"id"`
	OrganizationID string `json:"organization_id"`
	TenantID string `json:"tenant_id"`
	IdentityID string `json:"identity_id"`
	IdentityRole string `json:"identity_role"`
	Role string `json:"role"`
	Status string `json:"status"`
	CreatedAt time.Time `json:"created_at"`
}

func validOrganizationRole(role string) bool {
	switch role { case "owner","admin","manager","member","viewer": return true }
	return false
}
func validIdentityRole(role string) bool {
	switch role { case "customer","professional","operator","support": return true }
	return false
}

func appendOrganizationAudit(ctx context.Context, tx pgx.Tx, organizationID, tenantID, actorID, actorRole, action string, details any) error {
	raw, err := json.Marshal(details)
	if err != nil { return err }
	id, err := newID()
	if err != nil { return err }
	_, err = tx.Exec(ctx,`INSERT INTO organization_audit_events
		(id,organization_id,tenant_id,actor_identity_id,actor_identity_role,action,details)
		VALUES($1,$2,$3,$4,$5,$6,$7::jsonb)`,id,organizationID,tenantID,actorID,actorRole,action,raw)
	return err
}

func appendOrganizationDomainEvent(ctx context.Context, tx pgx.Tx, tenantID, aggregateID, eventType string, data any) error {
	raw, err := json.Marshal(data)
	if err != nil { return err }
	id, err := newID()
	if err != nil { return err }
	return outbox.AppendDomainEvent(ctx,tx,outbox.DomainEvent{
		ID:id, Type:eventType, Version:1, OccurredAt:time.Now().UTC(), TenantID:tenantID,
		Aggregate:outbox.EventAggregate{Type:"organization",ID:aggregateID}, Data:raw,
		Metadata:map[string]string{"source":"marketplace-api"},
	})
}

func (s *Store) CreateOrganizationForIdentity(ctx context.Context, identityID, tenantID, name, slug string) (OrganizationSummary,error) {
	name, slug = strings.TrimSpace(name), strings.ToLower(strings.TrimSpace(slug))
	if identityID=="" || tenantID=="" || len(name)<2 || len(name)>160 || !organizationSlugPattern.MatchString(slug) || len(slug)>120 {
		return OrganizationSummary{},ErrOrganizationInvalid
	}
	tx,err:=s.DB.BeginTx(ctx,pgx.TxOptions{}); if err!=nil{return OrganizationSummary{},err}; defer tx.Rollback(ctx)
	var identityRole string
	err=tx.QueryRow(ctx,`SELECT role FROM identity_memberships WHERE identity_id=$1 AND tenant_id=$2 AND role='customer'`,identityID,tenantID).Scan(&identityRole)
	if errors.Is(err,pgx.ErrNoRows){return OrganizationSummary{},ErrOrganizationForbidden}; if err!=nil{return OrganizationSummary{},err}
	orgID,err:=newID();if err!=nil{return OrganizationSummary{},err}
	_,err=tx.Exec(ctx,`INSERT INTO organizations(id,tenant_id,name,slug,created_by_identity_id,created_by_identity_role)
		VALUES($1,$2,$3,$4,$5,$6)`,orgID,tenantID,name,slug,identityID,identityRole)
	if err!=nil {
		if strings.Contains(strings.ToLower(err.Error()),"unique"){return OrganizationSummary{},ErrOrganizationConflict}
		return OrganizationSummary{},err
	}
	membershipID,err:=newID();if err!=nil{return OrganizationSummary{},err}
	_,err=tx.Exec(ctx,`INSERT INTO organization_memberships(id,organization_id,tenant_id,identity_id,identity_role,role,status)
		VALUES($1,$2,$3,$4,$5,'owner','active')`,membershipID,orgID,tenantID,identityID,identityRole)
	if err!=nil{return OrganizationSummary{},err}
	if err=appendOrganizationAudit(ctx,tx,orgID,tenantID,identityID,identityRole,"organization.created",map[string]string{"slug":slug});err!=nil{return OrganizationSummary{},err}
	if err=appendOrganizationDomainEvent(ctx,tx,tenantID,orgID,"organization.created",map[string]string{"organization_id":orgID,"status":"active"});err!=nil{return OrganizationSummary{},err}
	if err=tx.Commit(ctx);err!=nil{return OrganizationSummary{},err}
	return OrganizationSummary{ID:orgID,TenantID:tenantID,Name:name,Slug:slug,Status:"active",Role:"owner"},nil
}


func (s *Store) CreatePropertyForIdentity(ctx context.Context, identityID, tenantID, organizationID string, in PropertyCreateInput) (PropertySummary,error) {
	in.Code=strings.TrimSpace(in.Code); in.Name=strings.TrimSpace(in.Name)
	in.CountryCode=strings.ToUpper(strings.TrimSpace(in.CountryCode))
	in.RegionCode=strings.TrimSpace(in.RegionCode); in.CityCode=strings.TrimSpace(in.CityCode); in.AreaCode=strings.TrimSpace(in.AreaCode)
	if identityID==""||tenantID==""||organizationID==""||len(in.Code)<1||len(in.Code)>80||len(in.Name)<2||len(in.Name)>160||len(in.CountryCode)!=2||in.RegionCode==""||in.CityCode==""||in.AreaCode==""||len(in.RegionCode)>100||len(in.CityCode)>100||len(in.AreaCode)>100 {
		return PropertySummary{},ErrOrganizationInvalid
	}
	tx,err:=s.DB.BeginTx(ctx,pgx.TxOptions{});if err!=nil{return PropertySummary{},err};defer tx.Rollback(ctx)
	var actorRole,actorIdentityRole string
	err=tx.QueryRow(ctx,`SELECT om.role,om.identity_role FROM organization_memberships om JOIN organizations o ON o.id=om.organization_id AND o.tenant_id=om.tenant_id AND o.status='active' WHERE om.organization_id=$1 AND om.tenant_id=$2 AND om.identity_id=$3 AND om.status='active' AND om.role IN ('owner','admin','manager') FOR UPDATE OF om`,organizationID,tenantID,identityID).Scan(&actorRole,&actorIdentityRole)
	if errors.Is(err,pgx.ErrNoRows){return PropertySummary{},ErrOrganizationForbidden};if err!=nil{return PropertySummary{},err}
	id,err:=newID();if err!=nil{return PropertySummary{},err}
	_,err=tx.Exec(ctx,`INSERT INTO properties(id,tenant_id,organization_id,property_code,name,country_code,region_code,city_code,area_code,created_by_identity_id,created_by_identity_role)
		SELECT $1,$2,$3,$4,$5,$6,$7,$8,$9,$10,identity_role FROM organization_memberships
		WHERE organization_id=$3 AND tenant_id=$2 AND identity_id=$10 AND status='active'`,
		id,tenantID,organizationID,in.Code,in.Name,in.CountryCode,in.RegionCode,in.CityCode,in.AreaCode,identityID)
	if err!=nil {
		if strings.Contains(strings.ToLower(err.Error()),"unique"){return PropertySummary{},ErrOrganizationConflict}
		return PropertySummary{},err
	}
	if err=appendOrganizationAudit(ctx,tx,organizationID,tenantID,identityID,actorIdentityRole,"property.created",map[string]string{"property_id":id,"property_code":in.Code});err!=nil{return PropertySummary{},err}
	if err=appendOrganizationDomainEvent(ctx,tx,tenantID,organizationID,"property.created",map[string]string{"organization_id":organizationID,"property_id":id,"property_code":in.Code});err!=nil{return PropertySummary{},err}
	if err=tx.Commit(ctx);err!=nil{return PropertySummary{},err}
	return PropertySummary{ID:id,OrganizationID:organizationID,TenantID:tenantID,Code:in.Code,Name:in.Name,CountryCode:in.CountryCode,RegionCode:in.RegionCode,CityCode:in.CityCode,AreaCode:in.AreaCode,Status:"active",AccessRole:actorRole},nil
}

func (s *Store) InviteOrganizationMember(ctx context.Context, actorID, tenantID, organizationID string, in OrganizationInvitationInput) (OrganizationMembershipSummary,error) {
	in.IdentityID=strings.TrimSpace(in.IdentityID);in.IdentityRole=strings.TrimSpace(in.IdentityRole);in.Role=strings.TrimSpace(in.Role)
	if actorID==""||tenantID==""||organizationID==""||in.IdentityID==""||!validIdentityRole(in.IdentityRole)||!validOrganizationRole(in.Role)||in.Role=="owner" {
		return OrganizationMembershipSummary{},ErrOrganizationInvalid
	}
	tx,err:=s.DB.BeginTx(ctx,pgx.TxOptions{});if err!=nil{return OrganizationMembershipSummary{},err};defer tx.Rollback(ctx)
	var actorRole,actorIdentityRole string
	err=tx.QueryRow(ctx,`SELECT role,identity_role FROM organization_memberships WHERE organization_id=$1 AND tenant_id=$2 AND identity_id=$3 AND status='active' FOR UPDATE`,organizationID,tenantID,actorID).Scan(&actorRole,&actorIdentityRole)
	if errors.Is(err,pgx.ErrNoRows){return OrganizationMembershipSummary{},ErrOrganizationForbidden};if err!=nil{return OrganizationMembershipSummary{},err}
	if actorRole!="owner"&&actorRole!="admin"&&actorRole!="manager"{return OrganizationMembershipSummary{},ErrOrganizationForbidden}
	if actorRole=="manager"&&(in.Role!="member"&&in.Role!="viewer"){return OrganizationMembershipSummary{},ErrOrganizationForbidden}
	var targetExists bool
	err=tx.QueryRow(ctx,`SELECT EXISTS(SELECT 1 FROM identity_memberships WHERE identity_id=$1 AND tenant_id=$2 AND role=$3)`,in.IdentityID,tenantID,in.IdentityRole).Scan(&targetExists)
	if err!=nil{return OrganizationMembershipSummary{},err};if !targetExists{return OrganizationMembershipSummary{},ErrOrganizationInvalid}
	id,err:=newID();if err!=nil{return OrganizationMembershipSummary{},err}
	var result OrganizationMembershipSummary
	err=tx.QueryRow(ctx,`INSERT INTO organization_memberships(id,organization_id,tenant_id,identity_id,identity_role,role,status,invited_by_identity_id,invited_by_identity_role)
		VALUES($1,$2,$3,$4,$5,$6,'invited',$7,$8)
		RETURNING id,organization_id,tenant_id,identity_id,identity_role,role,status,created_at`,
		id,organizationID,tenantID,in.IdentityID,in.IdentityRole,in.Role,actorID,actorIdentityRole).
		Scan(&result.ID,&result.OrganizationID,&result.TenantID,&result.IdentityID,&result.IdentityRole,&result.Role,&result.Status,&result.CreatedAt)
	if err!=nil {
		if strings.Contains(strings.ToLower(err.Error()),"unique"){return OrganizationMembershipSummary{},ErrOrganizationConflict}
		return OrganizationMembershipSummary{},err
	}
	if err=appendOrganizationAudit(ctx,tx,organizationID,tenantID,actorID,actorIdentityRole,"membership.invited",map[string]string{"membership_id":id,"role":in.Role});err!=nil{return OrganizationMembershipSummary{},err}
	if err=appendOrganizationDomainEvent(ctx,tx,tenantID,organizationID,"organization.member_invited",map[string]string{"organization_id":organizationID,"membership_id":id,"role":in.Role});err!=nil{return OrganizationMembershipSummary{},err}
	if err=tx.Commit(ctx);err!=nil{return OrganizationMembershipSummary{},err}
	return result,nil
}

func (s *Store) ListPendingOrganizationInvitations(ctx context.Context, identityID, tenantID string) ([]OrganizationMembershipSummary,error) {
	rows,err:=s.DB.Query(ctx,`SELECT om.id,om.organization_id,om.tenant_id,om.identity_id,om.identity_role,om.role,om.status,om.created_at
		FROM organization_memberships om JOIN organizations o ON o.id=om.organization_id AND o.tenant_id=om.tenant_id
		JOIN identity_memberships im ON im.identity_id=om.identity_id AND im.tenant_id=om.tenant_id AND im.role=om.identity_role
		WHERE om.identity_id=$1 AND om.tenant_id=$2 AND om.status='invited' AND o.status='active'
		ORDER BY om.created_at,om.id LIMIT 100`,identityID,tenantID)
	if err!=nil{return nil,err};defer rows.Close()
	items:=make([]OrganizationMembershipSummary,0)
	for rows.Next(){var item OrganizationMembershipSummary;if err=rows.Scan(&item.ID,&item.OrganizationID,&item.TenantID,&item.IdentityID,&item.IdentityRole,&item.Role,&item.Status,&item.CreatedAt);err!=nil{return nil,err};items=append(items,item)}
	return items,rows.Err()
}

func (s *Store) AcceptOrganizationInvitation(ctx context.Context, identityID, tenantID, organizationID, membershipID string) error {
	tx,err:=s.DB.BeginTx(ctx,pgx.TxOptions{});if err!=nil{return err};defer tx.Rollback(ctx)
	var orgID,identityRole string
	err=tx.QueryRow(ctx,`UPDATE organization_memberships SET status='active',updated_at=now()
		WHERE id=$1 AND tenant_id=$2 AND organization_id=$3 AND identity_id=$4 AND status='invited'
		RETURNING organization_id,identity_role`,membershipID,tenantID,organizationID,identityID).Scan(&orgID,&identityRole)
	if errors.Is(err,pgx.ErrNoRows){return ErrOrganizationForbidden};if err!=nil{return err}
	if err=appendOrganizationAudit(ctx,tx,orgID,tenantID,identityID,identityRole,"membership.accepted",map[string]string{"membership_id":membershipID});err!=nil{return err}
	if err=appendOrganizationDomainEvent(ctx,tx,tenantID,orgID,"organization.member_accepted",map[string]string{"organization_id":orgID,"membership_id":membershipID});err!=nil{return err}
	return tx.Commit(ctx)
}

func (s *Store) ChangeOrganizationMemberRole(ctx context.Context,actorID,tenantID,organizationID,membershipID,newRole string) error {
	if !validOrganizationRole(newRole)||newRole=="owner"{return ErrOrganizationInvalid}
	tx,err:=s.DB.BeginTx(ctx,pgx.TxOptions{});if err!=nil{return err};defer tx.Rollback(ctx)
	var actorRole,actorIdentityRole,targetRole string
	err=tx.QueryRow(ctx,`SELECT role,identity_role FROM organization_memberships WHERE organization_id=$1 AND tenant_id=$2 AND identity_id=$3 AND status='active' FOR UPDATE`,organizationID,tenantID,actorID).Scan(&actorRole,&actorIdentityRole)
	if errors.Is(err,pgx.ErrNoRows){return ErrOrganizationForbidden};if err!=nil{return err}
	err=tx.QueryRow(ctx,`SELECT role FROM organization_memberships WHERE id=$1 AND organization_id=$2 AND tenant_id=$3 AND status='active' FOR UPDATE`,membershipID,organizationID,tenantID).Scan(&targetRole)
	if errors.Is(err,pgx.ErrNoRows){return ErrOrganizationForbidden};if err!=nil{return err}
	if targetRole=="owner"||actorRole=="manager"&&(targetRole!="member"&&targetRole!="viewer"||newRole!="member"&&newRole!="viewer")||actorRole=="admin"&&(targetRole=="admin"||newRole=="admin")||actorRole!="owner"&&actorRole!="admin"&&actorRole!="manager"{return ErrOrganizationForbidden}
	_,err=tx.Exec(ctx,`UPDATE organization_memberships SET role=$2,updated_at=now() WHERE id=$1`,membershipID,newRole);if err!=nil{return err}
	if err=appendOrganizationAudit(ctx,tx,organizationID,tenantID,actorID,actorIdentityRole,"membership.role_changed",map[string]string{"membership_id":membershipID,"from":targetRole,"to":newRole});err!=nil{return err}
	if err=appendOrganizationDomainEvent(ctx,tx,tenantID,organizationID,"organization.member_role_changed",map[string]string{"organization_id":organizationID,"membership_id":membershipID,"from":targetRole,"to":newRole});err!=nil{return err}
	return tx.Commit(ctx)
}

func (s *Store) RevokeOrganizationMember(ctx context.Context,actorID,tenantID,organizationID,membershipID string) error {
	tx,err:=s.DB.BeginTx(ctx,pgx.TxOptions{});if err!=nil{return err};defer tx.Rollback(ctx)
	var actorRole,actorIdentityRole,targetRole string
	err=tx.QueryRow(ctx,`SELECT role,identity_role FROM organization_memberships WHERE organization_id=$1 AND tenant_id=$2 AND identity_id=$3 AND status='active' FOR UPDATE`,organizationID,tenantID,actorID).Scan(&actorRole,&actorIdentityRole)
	if errors.Is(err,pgx.ErrNoRows){return ErrOrganizationForbidden};if err!=nil{return err}
	err=tx.QueryRow(ctx,`SELECT role FROM organization_memberships WHERE id=$1 AND organization_id=$2 AND tenant_id=$3 AND status IN ('active','invited') FOR UPDATE`,membershipID,organizationID,tenantID).Scan(&targetRole)
	if errors.Is(err,pgx.ErrNoRows){return ErrOrganizationForbidden};if err!=nil{return err}
	if targetRole=="owner"||actorRole!="owner"&&actorRole!="admin"&&actorRole!="manager"||actorRole=="manager"&&(targetRole!="member"&&targetRole!="viewer")||actorRole=="admin"&&targetRole=="admin"{return ErrOrganizationForbidden}
	_,err=tx.Exec(ctx,`UPDATE organization_memberships SET status='revoked',revoked_at=now(),updated_at=now() WHERE id=$1`,membershipID);if err!=nil{return err}
	_,err=tx.Exec(ctx,`UPDATE property_memberships SET status='revoked',revoked_at=now() WHERE organization_id=$1 AND tenant_id=$2 AND identity_id=(SELECT identity_id FROM organization_memberships WHERE id=$3) AND status='active'`,organizationID,tenantID,membershipID);if err!=nil{return err}
	if err=appendOrganizationAudit(ctx,tx,organizationID,tenantID,actorID,actorIdentityRole,"membership.revoked",map[string]string{"membership_id":membershipID,"role":targetRole});err!=nil{return err}
	if err=appendOrganizationDomainEvent(ctx,tx,tenantID,organizationID,"organization.member_revoked",map[string]string{"organization_id":organizationID,"membership_id":membershipID});err!=nil{return err}
	return tx.Commit(ctx)
}

func (h *Handler) organizationCreate(w http.ResponseWriter,r *http.Request) {
	p,err:=h.principal(r);if err!=nil{writeError(w,http.StatusUnauthorized,"unauthorized");return}
	var in OrganizationCreateInput
	decoder:=json.NewDecoder(http.MaxBytesReader(w,r.Body,16*1024));decoder.DisallowUnknownFields()
	if decoder.Decode(&in)!=nil{writeError(w,http.StatusBadRequest,"invalid request body");return}
	item,err:=h.Store.CreateOrganizationForIdentity(r.Context(),p.IdentityID,h.organizationTenant(r),in.Name,in.Slug)
	if errors.Is(err,ErrOrganizationInvalid){writeError(w,http.StatusBadRequest,err.Error());return}
	if errors.Is(err,ErrOrganizationForbidden){writeError(w,http.StatusForbidden,"active customer membership is required");return}
	if errors.Is(err,ErrOrganizationConflict){writeError(w,http.StatusConflict,"organization slug already exists");return}
	if err!=nil{writeError(w,http.StatusInternalServerError,"could not create organization");return}
	writeJSON(w,http.StatusCreated,map[string]any{"organization":item})
}

func (h *Handler) propertyCreate(w http.ResponseWriter,r *http.Request,orgID string) {
	p,err:=h.principal(r);if err!=nil{writeError(w,http.StatusUnauthorized,"unauthorized");return}
	var in PropertyCreateInput
	decoder:=json.NewDecoder(http.MaxBytesReader(w,r.Body,16*1024));decoder.DisallowUnknownFields()
	if decoder.Decode(&in)!=nil{writeError(w,http.StatusBadRequest,"invalid request body");return}
	item,err:=h.Store.CreatePropertyForIdentity(r.Context(),p.IdentityID,h.organizationTenant(r),orgID,in)
	if errors.Is(err,ErrOrganizationInvalid){writeError(w,http.StatusBadRequest,err.Error());return}
	if errors.Is(err,ErrOrganizationForbidden){writeError(w,http.StatusForbidden,"organization manager membership is required");return}
	if errors.Is(err,ErrOrganizationConflict){writeError(w,http.StatusConflict,"property code already exists");return}
	if err!=nil{writeError(w,http.StatusInternalServerError,"could not create property");return}
	writeJSON(w,http.StatusCreated,map[string]any{"property":item})
}

func (h *Handler) organizationInvite(w http.ResponseWriter,r *http.Request,orgID string) {
	p,err:=h.principal(r);if err!=nil{writeError(w,http.StatusUnauthorized,"unauthorized");return}
	var in OrganizationInvitationInput
	decoder:=json.NewDecoder(http.MaxBytesReader(w,r.Body,16*1024));decoder.DisallowUnknownFields()
	if decoder.Decode(&in)!=nil{writeError(w,http.StatusBadRequest,"invalid request body");return}
	item,err:=h.Store.InviteOrganizationMember(r.Context(),p.IdentityID,h.organizationTenant(r),orgID,in)
	if errors.Is(err,ErrOrganizationInvalid){writeError(w,http.StatusBadRequest,err.Error());return}
	if errors.Is(err,ErrOrganizationForbidden){writeError(w,http.StatusForbidden,"membership does not permit invitations");return}
	if errors.Is(err,ErrOrganizationConflict){writeError(w,http.StatusConflict,"membership already exists");return}
	if err!=nil{writeError(w,http.StatusInternalServerError,"could not invite member");return}
	writeJSON(w,http.StatusCreated,map[string]any{"invitation":item,"delivery":"in_app"})
}

func (h *Handler) organizationInvitations(w http.ResponseWriter,r *http.Request) {
	p,err:=h.principal(r);if err!=nil{writeError(w,http.StatusUnauthorized,"unauthorized");return}
	items,err:=h.Store.ListPendingOrganizationInvitations(r.Context(),p.IdentityID,h.organizationTenant(r))
	if err!=nil{writeError(w,http.StatusInternalServerError,"could not load invitations");return}
	writeJSON(w,http.StatusOK,map[string]any{"invitations":items})
}

func (h *Handler) organizationMembershipAction(w http.ResponseWriter,r *http.Request,orgID,membershipID,action string) {
	p,err:=h.principal(r);if err!=nil{writeError(w,http.StatusUnauthorized,"unauthorized");return}
	tenantID:=h.organizationTenant(r)
	switch action {
	case "accept":
		err=h.Store.AcceptOrganizationInvitation(r.Context(),p.IdentityID,tenantID,orgID,membershipID)
	case "revoke":
		err=h.Store.RevokeOrganizationMember(r.Context(),p.IdentityID,tenantID,orgID,membershipID)
	case "role":
		var in struct{Role string `json:"role"`}
		decoder:=json.NewDecoder(http.MaxBytesReader(w,r.Body,4096));decoder.DisallowUnknownFields()
		if decoder.Decode(&in)!=nil{writeError(w,http.StatusBadRequest,"invalid request body");return}
		err=h.Store.ChangeOrganizationMemberRole(r.Context(),p.IdentityID,tenantID,orgID,membershipID,in.Role)
	default:
		writeError(w,http.StatusNotFound,"not found");return
	}
	if errors.Is(err,ErrOrganizationInvalid){writeError(w,http.StatusBadRequest,err.Error());return}
	if errors.Is(err,ErrOrganizationForbidden){writeError(w,http.StatusForbidden,"membership operation is not permitted");return}
	if err!=nil{writeError(w,http.StatusInternalServerError,"could not update membership");return}
	status:=action
	if action=="accept"{status="accepted"} else if action=="revoke"{status="revoked"} else if action=="role"{status="updated"}
	writeJSON(w,http.StatusOK,map[string]string{"status":status})
}

func parseOrganizationManagementPath(path string)(orgID,resourceID,action string,ok bool){
	parts:=strings.Split(strings.Trim(path,"/"),"/")
	if len(parts)==3&&parts[0]=="v1"&&parts[1]=="organizations"&&parts[2]!=""{return parts[2],"","",true}
	if len(parts)==4&&parts[0]=="v1"&&parts[1]=="organizations"&&parts[2]!=""&&parts[3]=="properties"{return parts[2],"","properties",true}
	if len(parts)==4&&parts[0]=="v1"&&parts[1]=="organizations"&&parts[2]!=""&&parts[3]=="invitations"{return parts[2],"","invitations",true}
	if len(parts)==6&&parts[0]=="v1"&&parts[1]=="organizations"&&parts[2]!=""&&parts[3]=="members"&&parts[4]!=""{return parts[2],parts[4],parts[5],true}
	return "","","",false
}

