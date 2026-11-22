package marketplace

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

type UpdateOrganizationInput struct {
	Name *string `json:"name,omitempty"`
	Slug *string `json:"slug,omitempty"`
}

type UpdatePropertyInput struct {
	Code *string `json:"property_code,omitempty"`
	Name *string `json:"name,omitempty"`
	CountryCode *string `json:"country_code,omitempty"`
	RegionCode *string `json:"region_code,omitempty"`
	CityCode *string `json:"city_code,omitempty"`
	AreaCode *string `json:"area_code,omitempty"`
}

type PropertyDelegationInput struct {
	IdentityID string `json:"identity_id"`
	Role string `json:"role"`
}

type PropertyDelegationSummary struct {
	ID string `json:"id"`
	PropertyID string `json:"property_id"`
	OrganizationID string `json:"organization_id"`
	IdentityID string `json:"identity_id"`
	Role string `json:"role"`
	Status string `json:"status"`
	CreatedAt time.Time `json:"created_at"`
	RevokedAt *time.Time `json:"revoked_at,omitempty"`
}

func validateOrganizationPatch(in UpdateOrganizationInput) error {
	if in.Name == nil && in.Slug == nil { return ErrOrganizationInput }
	if in.Name != nil {
		*in.Name = strings.TrimSpace(*in.Name)
		if len(*in.Name) < 1 || len(*in.Name) > 160 { return ErrOrganizationInput }
	}
	if in.Slug != nil {
		*in.Slug = strings.TrimSpace(*in.Slug)
		if len(*in.Slug) < 1 || len(*in.Slug) > 160 || !organizationSlugPattern.MatchString(*in.Slug) { return ErrOrganizationInput }
	}
	return nil
}

func normalizePropertyPatch(in UpdatePropertyInput) (UpdatePropertyInput,error) {
	hasField := false
	normalize := func(v *string, upper bool, max int) error {
		if v == nil { return nil }
		hasField = true
		*v = strings.TrimSpace(*v)
		if upper { *v = strings.ToUpper(*v) }
		if len(*v) < 1 || len(*v) > max { return ErrOrganizationInput }
		return nil
	}
	if err:=normalize(in.Code,false,80);err!=nil{return in,err}
	if err:=normalize(in.Name,false,160);err!=nil{return in,err}
	if err:=normalize(in.CountryCode,true,2);err!=nil{return in,err}
	if err:=normalize(in.RegionCode,false,100);err!=nil{return in,err}
	if err:=normalize(in.CityCode,false,100);err!=nil{return in,err}
	if err:=normalize(in.AreaCode,false,100);err!=nil{return in,err}
	if !hasField{return in,ErrOrganizationInput}
	if in.CountryCode!=nil&&!countryCodePattern.MatchString(*in.CountryCode){return in,ErrOrganizationInput}
	return in,nil
}

func lockOrganizationActor(ctx context.Context,tx pgx.Tx,actorID,tenantID,organizationID string)(string,string,error){
	var actorRole,identityRole string
	err:=tx.QueryRow(ctx,"SELECT om.role,om.identity_role FROM organizations o JOIN organization_memberships om ON om.organization_id=o.id AND om.tenant_id=o.tenant_id WHERE o.id=$1 AND o.tenant_id=$2 AND o.status='active' AND om.identity_id=$3 AND om.status='active' FOR UPDATE OF o,om",organizationID,tenantID,actorID).Scan(&actorRole,&identityRole)
	if errors.Is(err,pgx.ErrNoRows){return "","",ErrOrganizationWriteForbidden}
	if err!=nil{return "","",err}
	return actorRole,identityRole,nil
}

func lockActiveProperty(ctx context.Context,tx pgx.Tx,tenantID,organizationID,propertyID string)(PropertySummary,error){
	var item PropertySummary
	err:=tx.QueryRow(ctx,"SELECT p.id,p.organization_id,p.tenant_id,p.property_code,p.name,p.country_code,p.region_code,p.city_code,p.area_code,p.status FROM properties p JOIN organizations o ON o.id=p.organization_id AND o.tenant_id=p.tenant_id WHERE p.id=$1 AND p.organization_id=$2 AND p.tenant_id=$3 AND p.status='active' AND o.status='active' FOR UPDATE OF p",propertyID,organizationID,tenantID).Scan(&item.ID,&item.OrganizationID,&item.TenantID,&item.Code,&item.Name,&item.CountryCode,&item.RegionCode,&item.CityCode,&item.AreaCode,&item.Status)
	if errors.Is(err,pgx.ErrNoRows){return PropertySummary{},ErrMembershipNotFound}
	if err!=nil{return PropertySummary{},err}
	return item,nil
}

func (s *Store) UpdateOrganizationForIdentity(ctx context.Context,actorID,tenantID,organizationID string,in UpdateOrganizationInput)(OrganizationSummary,error){
	if strings.TrimSpace(actorID)==""||strings.TrimSpace(tenantID)==""||strings.TrimSpace(organizationID)==""||validateOrganizationPatch(in)!=nil{return OrganizationSummary{},ErrOrganizationInput}
	tx,err:=s.DB.BeginTx(ctx,pgx.TxOptions{});if err!=nil{return OrganizationSummary{},err};defer tx.Rollback(ctx)
	actorRole,identityRole,err:=lockOrganizationActor(ctx,tx,actorID,tenantID,organizationID);if err!=nil{return OrganizationSummary{},err}
	if actorRole!="owner"&&actorRole!="admin"{return OrganizationSummary{},ErrMembershipForbidden}
	var name,slug string
	if err=tx.QueryRow(ctx,"SELECT name,slug FROM organizations WHERE id=$1 AND tenant_id=$2 AND status='active'",organizationID,tenantID).Scan(&name,&slug);err!=nil{return OrganizationSummary{},err}
	details:=map[string]string{"organization_id":organizationID}
	if in.Name!=nil{name=*in.Name;details["name"]=name}
	if in.Slug!=nil{slug=*in.Slug;details["slug"]=slug}
	if _,err=tx.Exec(ctx,"UPDATE organizations SET name=$1,slug=$2,updated_at=now() WHERE id=$3 AND tenant_id=$4 AND status='active'",name,slug,organizationID,tenantID);err!=nil{return OrganizationSummary{},err}
	if err=appendOrganizationAudit(ctx,tx,organizationID,tenantID,actorID,identityRole,"organization.updated",details);err!=nil{return OrganizationSummary{},err}
	if err=appendOrganizationDomainEvent(ctx,tx,tenantID,"organization",organizationID,"organization.updated",details);err!=nil{return OrganizationSummary{},err}
	if err=tx.Commit(ctx);err!=nil{return OrganizationSummary{},err}
	return OrganizationSummary{ID:organizationID,TenantID:tenantID,Name:name,Slug:slug,Status:"active",Role:actorRole},nil
}

// ArchiveOrganizationForIdentity is a soft, terminal archive. It preserves
// properties and downstream order/ledger history while revoking all access.
func (s *Store) ArchiveOrganizationForIdentity(ctx context.Context,actorID,tenantID,organizationID string)error{
	if strings.TrimSpace(actorID)==""||strings.TrimSpace(tenantID)==""||strings.TrimSpace(organizationID)==""{return ErrOrganizationInput}
	tx,err:=s.DB.BeginTx(ctx,pgx.TxOptions{});if err!=nil{return err};defer tx.Rollback(ctx)
	actorRole,identityRole,err:=lockOrganizationActor(ctx,tx,actorID,tenantID,organizationID);if err!=nil{return err}
	if actorRole!="owner"{return ErrMembershipForbidden}
	details:=map[string]string{"organization_id":organizationID,"status":"archived"}
	if err=appendOrganizationAudit(ctx,tx,organizationID,tenantID,actorID,identityRole,"organization.archived",details);err!=nil{return err}
	if err=appendOrganizationDomainEvent(ctx,tx,tenantID,"organization",organizationID,"organization.archived",details);err!=nil{return err}
	if _,err=tx.Exec(ctx,"UPDATE properties SET status='archived',updated_at=now() WHERE organization_id=$1 AND tenant_id=$2 AND status='active'",organizationID,tenantID);err!=nil{return err}
	if _,err=tx.Exec(ctx,"UPDATE property_memberships SET status='revoked',revoked_at=now() WHERE organization_id=$1 AND tenant_id=$2 AND status='active'",organizationID,tenantID);err!=nil{return err}
	if _,err=tx.Exec(ctx,"UPDATE organization_memberships SET status='revoked',revoked_at=now(),updated_at=now() WHERE organization_id=$1 AND tenant_id=$2 AND status IN ('active','invited')",organizationID,tenantID);err!=nil{return err}
	if _,err=tx.Exec(ctx,"UPDATE organizations SET status='archived',updated_at=now() WHERE id=$1 AND tenant_id=$2 AND status='active'",organizationID,tenantID);err!=nil{return err}
	return tx.Commit(ctx)
}

func (s *Store) UpdatePropertyForIdentity(ctx context.Context,actorID,tenantID,organizationID,propertyID string,in UpdatePropertyInput)(PropertySummary,error){
	in,validationErr:=normalizePropertyPatch(in)
	if strings.TrimSpace(actorID)==""||strings.TrimSpace(tenantID)==""||strings.TrimSpace(organizationID)==""||strings.TrimSpace(propertyID)==""||validationErr!=nil{return PropertySummary{},ErrOrganizationInput}
	tx,err:=s.DB.BeginTx(ctx,pgx.TxOptions{});if err!=nil{return PropertySummary{},err};defer tx.Rollback(ctx)
	actorRole,identityRole,err:=lockOrganizationActor(ctx,tx,actorID,tenantID,organizationID);if err!=nil{return PropertySummary{},err}
	if actorRole!="owner"&&actorRole!="admin"&&actorRole!="manager"{return PropertySummary{},ErrMembershipForbidden}
	item,err:=lockActiveProperty(ctx,tx,tenantID,organizationID,propertyID);if err!=nil{return PropertySummary{},err}
	if in.Code!=nil{item.Code=*in.Code};if in.Name!=nil{item.Name=*in.Name};if in.CountryCode!=nil{item.CountryCode=*in.CountryCode};if in.RegionCode!=nil{item.RegionCode=*in.RegionCode};if in.CityCode!=nil{item.CityCode=*in.CityCode};if in.AreaCode!=nil{item.AreaCode=*in.AreaCode}
	if _,err=tx.Exec(ctx,"UPDATE properties SET property_code=$1,name=$2,country_code=$3,region_code=$4,city_code=$5,area_code=$6,updated_at=now() WHERE id=$7 AND organization_id=$8 AND tenant_id=$9 AND status='active'",item.Code,item.Name,item.CountryCode,item.RegionCode,item.CityCode,item.AreaCode,propertyID,organizationID,tenantID);err!=nil{return PropertySummary{},err}
	details:=map[string]string{"property_id":propertyID,"organization_id":organizationID}
	if in.Code!=nil{details["property_code"]=item.Code};if in.Name!=nil{details["name"]=item.Name};if in.CountryCode!=nil{details["country_code"]=item.CountryCode};if in.RegionCode!=nil{details["region_code"]=item.RegionCode};if in.CityCode!=nil{details["city_code"]=item.CityCode};if in.AreaCode!=nil{details["area_code"]=item.AreaCode}
	if err=appendOrganizationAudit(ctx,tx,organizationID,tenantID,actorID,identityRole,"property.updated",details);err!=nil{return PropertySummary{},err}
	if err=appendOrganizationDomainEvent(ctx,tx,tenantID,"property",propertyID,"property.updated",details);err!=nil{return PropertySummary{},err}
	if err=tx.Commit(ctx);err!=nil{return PropertySummary{},err}
	item.AccessRole=actorRole
	return item,nil
}

func (s *Store) ArchivePropertyForIdentity(ctx context.Context,actorID,tenantID,organizationID,propertyID string)error{
	if strings.TrimSpace(actorID)==""||strings.TrimSpace(tenantID)==""||strings.TrimSpace(organizationID)==""||strings.TrimSpace(propertyID)==""{return ErrOrganizationInput}
	tx,err:=s.DB.BeginTx(ctx,pgx.TxOptions{});if err!=nil{return err};defer tx.Rollback(ctx)
	actorRole,identityRole,err:=lockOrganizationActor(ctx,tx,actorID,tenantID,organizationID);if err!=nil{return err}
	if actorRole!="owner"&&actorRole!="admin"&&actorRole!="manager"{return ErrMembershipForbidden}
	if _,err=lockActiveProperty(ctx,tx,tenantID,organizationID,propertyID);err!=nil{return err}
	details:=map[string]string{"property_id":propertyID,"organization_id":organizationID,"status":"archived"}
	if err=appendOrganizationAudit(ctx,tx,organizationID,tenantID,actorID,identityRole,"property.archived",details);err!=nil{return err}
	if err=appendOrganizationDomainEvent(ctx,tx,tenantID,"property",propertyID,"property.archived",details);err!=nil{return err}
	if _,err=tx.Exec(ctx,"UPDATE property_memberships SET status='revoked',revoked_at=now() WHERE property_id=$1 AND organization_id=$2 AND tenant_id=$3 AND status='active'",propertyID,organizationID,tenantID);err!=nil{return err}
	if _,err=tx.Exec(ctx,"UPDATE properties SET status='archived',updated_at=now() WHERE id=$1 AND organization_id=$2 AND tenant_id=$3 AND status='active'",propertyID,organizationID,tenantID);err!=nil{return err}
	return tx.Commit(ctx)
}

func (s *Store) ListPropertyDelegationsForIdentity(ctx context.Context,actorID,tenantID,organizationID,propertyID string)([]PropertyDelegationSummary,error){
	var actorRole string
	err:=s.DB.QueryRow(ctx,"SELECT om.role FROM organizations o JOIN organization_memberships om ON om.organization_id=o.id AND om.tenant_id=o.tenant_id JOIN properties p ON p.organization_id=o.id AND p.tenant_id=o.tenant_id WHERE o.id=$1 AND o.tenant_id=$2 AND o.status='active' AND om.identity_id=$3 AND om.status='active' AND p.id=$4 AND p.status='active' AND om.role IN ('owner','admin','manager')",organizationID,tenantID,actorID,propertyID).Scan(&actorRole)
	if errors.Is(err,pgx.ErrNoRows){return nil,ErrMembershipForbidden};if err!=nil{return nil,err}
	rows,err:=s.DB.Query(ctx,"SELECT pm.id,pm.property_id,pm.organization_id,pm.identity_id,pm.role,pm.status,pm.created_at,pm.revoked_at FROM property_memberships pm JOIN organization_memberships target ON target.organization_id=pm.organization_id AND target.tenant_id=pm.tenant_id AND target.identity_id=pm.identity_id AND target.identity_role=pm.identity_role WHERE pm.property_id=$1 AND pm.organization_id=$2 AND pm.tenant_id=$3 AND pm.status='active' AND target.status='active' ORDER BY pm.created_at,pm.id",propertyID,organizationID,tenantID)
	if err!=nil{return nil,err};defer rows.Close()
	items:=make([]PropertyDelegationSummary,0)
	for rows.Next(){var item PropertyDelegationSummary;if err=rows.Scan(&item.ID,&item.PropertyID,&item.OrganizationID,&item.IdentityID,&item.Role,&item.Status,&item.CreatedAt,&item.RevokedAt);err!=nil{return nil,err};items=append(items,item)}
	if err=rows.Err();err!=nil{return nil,err};return items,nil
}

func (s *Store) GrantPropertyDelegation(ctx context.Context,actorID,tenantID,organizationID,propertyID string,in PropertyDelegationInput)(PropertyDelegationSummary,error){
	in.IdentityID=strings.TrimSpace(in.IdentityID)
	if actorID==""||tenantID==""||organizationID==""||propertyID==""||in.IdentityID==""||(in.Role!="manager"&&in.Role!="viewer"){return PropertyDelegationSummary{},ErrOrganizationInput}
	tx,err:=s.DB.BeginTx(ctx,pgx.TxOptions{});if err!=nil{return PropertyDelegationSummary{},err};defer tx.Rollback(ctx)
	actorRole,actorIdentityRole,err:=lockOrganizationActor(ctx,tx,actorID,tenantID,organizationID);if err!=nil{return PropertyDelegationSummary{},err}
	if actorRole!="owner"&&actorRole!="admin"&&actorRole!="manager"{return PropertyDelegationSummary{},ErrMembershipForbidden}
	if actorRole=="manager"&&in.Role!="viewer"{return PropertyDelegationSummary{},ErrMembershipForbidden}
	if _,err=lockActiveProperty(ctx,tx,tenantID,organizationID,propertyID);err!=nil{return PropertyDelegationSummary{},err}
	var targetIdentityRole,targetOrgRole string
	err=tx.QueryRow(ctx,"SELECT identity_role,role FROM organization_memberships WHERE organization_id=$1 AND tenant_id=$2 AND identity_id=$3 AND status='active' FOR UPDATE",organizationID,tenantID,in.IdentityID).Scan(&targetIdentityRole,&targetOrgRole)
	if errors.Is(err,pgx.ErrNoRows){return PropertyDelegationSummary{},ErrMembershipNotFound};if err!=nil{return PropertyDelegationSummary{},err}
	if targetOrgRole!="member"&&targetOrgRole!="viewer"{return PropertyDelegationSummary{},ErrMembershipConflict}
	if in.IdentityID==actorID{return PropertyDelegationSummary{},ErrMembershipConflict}
	var id,status string
	err=tx.QueryRow(ctx,"SELECT id,status FROM property_memberships WHERE property_id=$1 AND tenant_id=$2 AND identity_id=$3 AND identity_role=$4 FOR UPDATE",propertyID,tenantID,in.IdentityID,targetIdentityRole).Scan(&id,&status)
	if err==nil{if status!="revoked"{return PropertyDelegationSummary{},ErrMembershipConflict};_,err=tx.Exec(ctx,"UPDATE property_memberships SET role=$1,status='active',revoked_at=NULL WHERE id=$2",in.Role,id)
	}else if errors.Is(err,pgx.ErrNoRows){id,err=newID();if err==nil{_,err=tx.Exec(ctx,"INSERT INTO property_memberships(id,property_id,organization_id,tenant_id,identity_id,identity_role,role,status,created_by_identity_id,created_by_identity_role) VALUES($1,$2,$3,$4,$5,$6,$7,'active',$8,$9)",id,propertyID,organizationID,tenantID,in.IdentityID,targetIdentityRole,in.Role,actorID,actorIdentityRole)}}
	if err!=nil{if isMembershipUniqueConflict(err){return PropertyDelegationSummary{},ErrMembershipConflict};return PropertyDelegationSummary{},err}
	details:=map[string]string{"property_id":propertyID,"identity_id":in.IdentityID,"role":in.Role}
	if err=appendOrganizationAudit(ctx,tx,organizationID,tenantID,actorID,actorIdentityRole,"property.member.granted",details);err!=nil{return PropertyDelegationSummary{},err}
	if err=appendOrganizationDomainEvent(ctx,tx,tenantID,"property",propertyID,"property.member.granted",details);err!=nil{return PropertyDelegationSummary{},err}
	var createdAt time.Time
	if err=tx.QueryRow(ctx,"SELECT created_at FROM property_memberships WHERE id=$1",id).Scan(&createdAt);err!=nil{return PropertyDelegationSummary{},err}
	if err=tx.Commit(ctx);err!=nil{return PropertyDelegationSummary{},err}
	return PropertyDelegationSummary{ID:id,PropertyID:propertyID,OrganizationID:organizationID,IdentityID:in.IdentityID,Role:in.Role,Status:"active",CreatedAt:createdAt},nil
}

func (s *Store) ChangePropertyDelegationRole(ctx context.Context,actorID,tenantID,organizationID,propertyID,targetID,nextRole string)(PropertyDelegationSummary,error){
	if nextRole!="manager"&&nextRole!="viewer"{return PropertyDelegationSummary{},ErrOrganizationInput}
	tx,err:=s.DB.BeginTx(ctx,pgx.TxOptions{});if err!=nil{return PropertyDelegationSummary{},err};defer tx.Rollback(ctx)
	actorRole,actorIdentityRole,err:=lockOrganizationActor(ctx,tx,actorID,tenantID,organizationID);if err!=nil{return PropertyDelegationSummary{},err}
	if actorRole!="owner"&&actorRole!="admin"{return PropertyDelegationSummary{},ErrMembershipForbidden}
	if _,err=lockActiveProperty(ctx,tx,tenantID,organizationID,propertyID);err!=nil{return PropertyDelegationSummary{},err}
	var targetIdentityRole,targetOrgRole string
	err=tx.QueryRow(ctx,"SELECT identity_role,role FROM organization_memberships WHERE organization_id=$1 AND tenant_id=$2 AND identity_id=$3 AND status='active' FOR UPDATE",organizationID,tenantID,targetID).Scan(&targetIdentityRole,&targetOrgRole)
	if errors.Is(err,pgx.ErrNoRows){return PropertyDelegationSummary{},ErrMembershipNotFound};if err!=nil{return PropertyDelegationSummary{},err}
	if targetOrgRole!="member"&&targetOrgRole!="viewer"{return PropertyDelegationSummary{},ErrMembershipConflict}
	var item PropertyDelegationSummary
	err=tx.QueryRow(ctx,"SELECT id,property_id,organization_id,identity_id,role,status,created_at,revoked_at FROM property_memberships WHERE property_id=$1 AND tenant_id=$2 AND identity_id=$3 AND identity_role=$4 AND status='active' FOR UPDATE",propertyID,tenantID,targetID,targetIdentityRole).Scan(&item.ID,&item.PropertyID,&item.OrganizationID,&item.IdentityID,&item.Role,&item.Status,&item.CreatedAt,&item.RevokedAt)
	if errors.Is(err,pgx.ErrNoRows){return PropertyDelegationSummary{},ErrMembershipNotFound};if err!=nil{return PropertyDelegationSummary{},err}
	if item.Role==nextRole{if err=tx.Commit(ctx);err!=nil{return PropertyDelegationSummary{},err};return item,nil}
	_,err=tx.Exec(ctx,"UPDATE property_memberships SET role=$1 WHERE id=$2",nextRole,item.ID);if err!=nil{return PropertyDelegationSummary{},err}
	details:=map[string]string{"property_id":propertyID,"identity_id":targetID,"previous_role":item.Role,"role":nextRole}
	if err=appendOrganizationAudit(ctx,tx,organizationID,tenantID,actorID,actorIdentityRole,"property.member.role_changed",details);err!=nil{return PropertyDelegationSummary{},err}
	if err=appendOrganizationDomainEvent(ctx,tx,tenantID,"property",propertyID,"property.member.role_changed",details);err!=nil{return PropertyDelegationSummary{},err}
	if err=tx.Commit(ctx);err!=nil{return PropertyDelegationSummary{},err};item.Role=nextRole;return item,nil
}

func (s *Store) RevokePropertyDelegation(ctx context.Context,actorID,tenantID,organizationID,propertyID,targetID string)error{
	tx,err:=s.DB.BeginTx(ctx,pgx.TxOptions{});if err!=nil{return err};defer tx.Rollback(ctx)
	actorRole,actorIdentityRole,err:=lockOrganizationActor(ctx,tx,actorID,tenantID,organizationID);if err!=nil{return err}
	if actorRole!="owner"&&actorRole!="admin"&&actorRole!="manager"{return ErrMembershipForbidden}
	if _,err=lockActiveProperty(ctx,tx,tenantID,organizationID,propertyID);err!=nil{return err}
	var targetOrgRole,delegationRole,delegationID string
	err=tx.QueryRow(ctx,"SELECT target.role,pm.role,pm.id FROM organization_memberships target JOIN property_memberships pm ON pm.organization_id=target.organization_id AND pm.tenant_id=target.tenant_id AND pm.identity_id=target.identity_id AND pm.identity_role=target.identity_role WHERE target.organization_id=$1 AND target.tenant_id=$2 AND target.identity_id=$3 AND target.status='active' AND pm.property_id=$4 AND pm.status='active' FOR UPDATE OF target,pm",organizationID,tenantID,targetID,propertyID).Scan(&targetOrgRole,&delegationRole,&delegationID)
	if errors.Is(err,pgx.ErrNoRows){return ErrMembershipNotFound};if err!=nil{return err}
	if targetOrgRole!="member"&&targetOrgRole!="viewer"{return ErrMembershipConflict}
	if actorRole=="manager"&&delegationRole!="viewer"{return ErrMembershipForbidden}
	if _,err=tx.Exec(ctx,"UPDATE property_memberships SET status='revoked',revoked_at=now() WHERE id=$1 AND status='active'",delegationID);err!=nil{return err}
	details:=map[string]string{"property_id":propertyID,"identity_id":targetID,"role":delegationRole}
	if err=appendOrganizationAudit(ctx,tx,organizationID,tenantID,actorID,actorIdentityRole,"property.member.revoked",details);err!=nil{return err}
	if err=appendOrganizationDomainEvent(ctx,tx,tenantID,"property",propertyID,"property.member.revoked",details);err!=nil{return err}
	return tx.Commit(ctx)
}
