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
 ErrPropertyMembershipForbidden = errors.New("property membership operation forbidden")
 ErrPropertyMembershipNotFound = errors.New("property membership not found")
 ErrPropertyMembershipConflict = errors.New("property membership state conflict")
)

type PropertyMembershipInput struct {
 IdentityID string `json:"identity_id"`
 Role string `json:"role"`
}
type PropertyMembershipResult struct {
 PropertyID string `json:"property_id"`
 OrganizationID string `json:"organization_id"`
 TenantID string `json:"tenant_id"`
 IdentityID string `json:"identity_id"`
 Role string `json:"role"`
 Status string `json:"status"`
}

func validPropertyMembershipRole(role string) bool { return role=="manager" || role=="viewer" }
func canManagePropertyMemberships(role string) bool { return role=="owner" || role=="admin" || role=="manager" }

func (s *Store) propertyMembershipActor(ctx context.Context, tx pgx.Tx, actorID,tenantID,organizationID string)(string,string,error){
 var role,identityRole string
 err:=tx.QueryRow(ctx,`
 SELECT om.role,om.identity_role FROM organizations o
 JOIN organization_memberships om ON om.organization_id=o.id AND om.tenant_id=o.tenant_id
 JOIN identity_memberships im ON im.identity_id=om.identity_id AND im.tenant_id=om.tenant_id AND im.role=om.identity_role
 WHERE o.id=$1 AND o.tenant_id=$2 AND o.status='active' AND om.identity_id=$3 AND om.status='active'
 FOR UPDATE OF o
 `,organizationID,tenantID,actorID).Scan(&role,&identityRole)
 if errors.Is(err,pgx.ErrNoRows){return "","",ErrPropertyMembershipForbidden}
 return role,identityRole,err
}

func lockActiveProperty(ctx context.Context,tx pgx.Tx,propertyID,organizationID,tenantID string)error{
 var status string
 err:=tx.QueryRow(ctx,`SELECT status FROM properties WHERE id=$1 AND organization_id=$2 AND tenant_id=$3 FOR UPDATE`,propertyID,organizationID,tenantID).Scan(&status)
 if errors.Is(err,pgx.ErrNoRows){return ErrPropertyMembershipNotFound}
 if err!=nil{return err}
 if status!="active"{return ErrPropertyMembershipNotFound}
 return nil
}

func (s *Store) ListPropertyMemberships(ctx context.Context,actorID,tenantID,organizationID,propertyID string)([]PropertyMembershipResult,error){
 tx,err:=s.DB.BeginTx(ctx,pgx.TxOptions{AccessMode:pgx.ReadOnly})
 if err!=nil{return nil,err}
 defer tx.Rollback(ctx)
 role,_,err:=s.propertyMembershipActor(ctx,tx,actorID,tenantID,organizationID)
 if err!=nil{return nil,err}
 if !canManagePropertyMemberships(role){return nil,ErrPropertyMembershipForbidden}
 if err=lockActiveProperty(ctx,tx,propertyID,organizationID,tenantID);err!=nil{return nil,err}
 rows,err:=tx.Query(ctx,`
 SELECT pm.property_id,pm.organization_id,pm.tenant_id,pm.identity_id,pm.role,pm.status
 FROM property_memberships pm
 JOIN organization_memberships om ON om.organization_id=pm.organization_id AND om.tenant_id=pm.tenant_id
  AND om.identity_id=pm.identity_id AND om.identity_role=pm.identity_role AND om.status='active'
 JOIN identity_memberships im ON im.identity_id=om.identity_id AND im.tenant_id=om.tenant_id AND im.role=om.identity_role
 WHERE pm.property_id=$1 AND pm.organization_id=$2 AND pm.tenant_id=$3 AND pm.status='active'
  AND om.role IN ('member','viewer')
 ORDER BY pm.identity_id
 `,propertyID,organizationID,tenantID)
 if err!=nil{return nil,err}
 defer rows.Close()
 items:=make([]PropertyMembershipResult,0)
 for rows.Next(){var item PropertyMembershipResult;if err=rows.Scan(&item.PropertyID,&item.OrganizationID,&item.TenantID,&item.IdentityID,&item.Role,&item.Status);err!=nil{return nil,err};items=append(items,item)}
 if err=rows.Err();err!=nil{return nil,err}
 if err=tx.Commit(ctx);err!=nil{return nil,err}
 return items,nil
}

func (s *Store) AssignPropertyMembership(ctx context.Context,actorID,tenantID,organizationID,propertyID string,in PropertyMembershipInput)(PropertyMembershipResult,error){
 in.IdentityID=strings.TrimSpace(in.IdentityID);in.Role=strings.TrimSpace(in.Role)
 if actorID==""||tenantID==""||organizationID==""||propertyID==""||in.IdentityID==""||in.IdentityID==actorID||!validPropertyMembershipRole(in.Role){return PropertyMembershipResult{},ErrOrganizationInput}
 tx,err:=s.DB.BeginTx(ctx,pgx.TxOptions{});if err!=nil{return PropertyMembershipResult{},err};defer tx.Rollback(ctx)
 actorRole,actorIdentityRole,err:=s.propertyMembershipActor(ctx,tx,actorID,tenantID,organizationID);if err!=nil{return PropertyMembershipResult{},err}
 if !canManagePropertyMemberships(actorRole){return PropertyMembershipResult{},ErrPropertyMembershipForbidden}
 if err=lockActiveProperty(ctx,tx,propertyID,organizationID,tenantID);err!=nil{return PropertyMembershipResult{},err}
 var targetRole,targetIdentityRole string
 err=tx.QueryRow(ctx,`
 SELECT om.role,om.identity_role FROM organization_memberships om
 JOIN identity_memberships im ON im.identity_id=om.identity_id AND im.tenant_id=om.tenant_id AND im.role=om.identity_role
 WHERE om.organization_id=$1 AND om.tenant_id=$2 AND om.identity_id=$3 AND om.status='active'
 FOR SHARE OF om,im
 `,organizationID,tenantID,in.IdentityID).Scan(&targetRole,&targetIdentityRole)
 if errors.Is(err,pgx.ErrNoRows){return PropertyMembershipResult{},ErrPropertyMembershipNotFound};if err!=nil{return PropertyMembershipResult{},err}
 if targetRole!="member"&&targetRole!="viewer"{return PropertyMembershipResult{},ErrPropertyMembershipForbidden}
 var id,status string
 err=tx.QueryRow(ctx,`SELECT id,status FROM property_memberships WHERE property_id=$1 AND organization_id=$2 AND tenant_id=$3 AND identity_id=$4 AND identity_role=$5 FOR UPDATE`,propertyID,organizationID,tenantID,in.IdentityID,targetIdentityRole).Scan(&id,&status)
 if err==nil{
  if status=="active"{return PropertyMembershipResult{},ErrPropertyMembershipConflict}
  _,err=tx.Exec(ctx,`UPDATE property_memberships SET role=$1,status='active',revoked_at=NULL WHERE id=$2`,in.Role,id)
 }else if errors.Is(err,pgx.ErrNoRows){
  id,err=newID()
  if err==nil{_,err=tx.Exec(ctx,`INSERT INTO property_memberships(id,property_id,organization_id,tenant_id,identity_id,identity_role,role,status,created_by_identity_id,created_by_identity_role) VALUES($1,$2,$3,$4,$5,$6,$7,'active',$8,$9)`,id,propertyID,organizationID,tenantID,in.IdentityID,targetIdentityRole,in.Role,actorID,actorIdentityRole)}
 }
 if err!=nil{var pe *pgconn.PgError;if errors.As(err,&pe)&&pe.Code=="23505"{return PropertyMembershipResult{},ErrPropertyMembershipConflict};return PropertyMembershipResult{},err}
 if err=writePropertyMembershipMutation(ctx,tx,tenantID,organizationID,propertyID,id,actorID,actorIdentityRole,in.IdentityID,"property.member.assigned",map[string]string{"role":in.Role});err!=nil{return PropertyMembershipResult{},err}
 if err=tx.Commit(ctx);err!=nil{return PropertyMembershipResult{},err}
 return PropertyMembershipResult{PropertyID:propertyID,OrganizationID:organizationID,TenantID:tenantID,IdentityID:in.IdentityID,Role:in.Role,Status:"active"},nil
}

func (s *Store) ChangePropertyMembershipRole(ctx context.Context,actorID,tenantID,organizationID,propertyID,targetID,nextRole string)(PropertyMembershipResult,error){
 nextRole=strings.TrimSpace(nextRole);if !validPropertyMembershipRole(nextRole){return PropertyMembershipResult{},ErrOrganizationInput}
 tx,err:=s.DB.BeginTx(ctx,pgx.TxOptions{});if err!=nil{return PropertyMembershipResult{},err};defer tx.Rollback(ctx)
 actorRole,actorIdentityRole,err:=s.propertyMembershipActor(ctx,tx,actorID,tenantID,organizationID);if err!=nil{return PropertyMembershipResult{},err}
 if !canManagePropertyMemberships(actorRole){return PropertyMembershipResult{},ErrPropertyMembershipForbidden}
 if err=lockActiveProperty(ctx,tx,propertyID,organizationID,tenantID);err!=nil{return PropertyMembershipResult{},err}
 var id,oldRole,status string
 err=tx.QueryRow(ctx,`
 SELECT pm.id,pm.role,pm.status FROM property_memberships pm
 JOIN organization_memberships om ON om.organization_id=pm.organization_id AND om.tenant_id=pm.tenant_id AND om.identity_id=pm.identity_id AND om.identity_role=pm.identity_role AND om.status='active'
 JOIN identity_memberships im ON im.identity_id=om.identity_id AND im.tenant_id=om.tenant_id AND im.role=om.identity_role
 WHERE pm.property_id=$1 AND pm.organization_id=$2 AND pm.tenant_id=$3 AND pm.identity_id=$4
 FOR UPDATE OF pm,om
 `,propertyID,organizationID,tenantID,targetID).Scan(&id,&oldRole,&status)
 if errors.Is(err,pgx.ErrNoRows){return PropertyMembershipResult{},ErrPropertyMembershipNotFound};if err!=nil{return PropertyMembershipResult{},err}
 if status!="active"{return PropertyMembershipResult{},ErrPropertyMembershipNotFound};if oldRole==nextRole{return PropertyMembershipResult{},ErrPropertyMembershipConflict}
 if _,err=tx.Exec(ctx,`UPDATE property_memberships SET role=$1 WHERE id=$2`,nextRole,id);err!=nil{return PropertyMembershipResult{},err}
 if err=writePropertyMembershipMutation(ctx,tx,tenantID,organizationID,propertyID,id,actorID,actorIdentityRole,targetID,"property.member.role_changed",map[string]string{"from":oldRole,"to":nextRole});err!=nil{return PropertyMembershipResult{},err}
 if err=tx.Commit(ctx);err!=nil{return PropertyMembershipResult{},err}
 return PropertyMembershipResult{PropertyID:propertyID,OrganizationID:organizationID,TenantID:tenantID,IdentityID:targetID,Role:nextRole,Status:"active"},nil
}

func (s *Store) RevokePropertyMembership(ctx context.Context,actorID,tenantID,organizationID,propertyID,targetID string)error{
 tx,err:=s.DB.BeginTx(ctx,pgx.TxOptions{});if err!=nil{return err};defer tx.Rollback(ctx)
 actorRole,actorIdentityRole,err:=s.propertyMembershipActor(ctx,tx,actorID,tenantID,organizationID);if err!=nil{return err}
 if !canManagePropertyMemberships(actorRole){return ErrPropertyMembershipForbidden}
 if err=lockActiveProperty(ctx,tx,propertyID,organizationID,tenantID);err!=nil{return err}
 var id,role,status string
 err=tx.QueryRow(ctx,`
 SELECT pm.id,pm.role,pm.status FROM property_memberships pm
 JOIN organization_memberships om ON om.organization_id=pm.organization_id AND om.tenant_id=pm.tenant_id AND om.identity_id=pm.identity_id AND om.identity_role=pm.identity_role AND om.status='active'
 JOIN identity_memberships im ON im.identity_id=om.identity_id AND im.tenant_id=om.tenant_id AND im.role=om.identity_role
 WHERE pm.property_id=$1 AND pm.organization_id=$2 AND pm.tenant_id=$3 AND pm.identity_id=$4
 FOR UPDATE OF pm,om
 `,propertyID,organizationID,tenantID,targetID).Scan(&id,&role,&status)
 if errors.Is(err,pgx.ErrNoRows){return ErrPropertyMembershipNotFound};if err!=nil{return err}
 if status!="active"{return ErrPropertyMembershipNotFound}
 if _,err=tx.Exec(ctx,`UPDATE property_memberships SET status='revoked',revoked_at=now() WHERE id=$1`,id);err!=nil{return err}
 if err=writePropertyMembershipMutation(ctx,tx,tenantID,organizationID,propertyID,id,actorID,actorIdentityRole,targetID,"property.member.revoked",map[string]string{"role":role});err!=nil{return err}
 return tx.Commit(ctx)
}

func writePropertyMembershipMutation(ctx context.Context,tx pgx.Tx,tenantID,organizationID,propertyID,membershipID,actorID,actorRole,targetID,eventType string,details map[string]string)error{
 details["property_id"]=propertyID;details["identity_id"]=targetID
 if err:=appendOrganizationAudit(ctx,tx,organizationID,tenantID,actorID,actorRole,eventType,details);err!=nil{return err}
 payload,err:=json.Marshal(details);if err!=nil{return err}
 eventID,err:=newID();if err!=nil{return err}
 return appendDomainEvent(ctx,tx,outbox.DomainEvent{ID:eventID,Type:eventType,Version:1,OccurredAt:time.Now().UTC(),TenantID:tenantID,Aggregate:outbox.EventAggregate{Type:"property_membership",ID:membershipID},Data:payload})
}

func (h *Handler) propertyMembershipRoutes(w http.ResponseWriter,r *http.Request,organizationID,propertyID string)bool{
 principal,err:=h.principal(r);if err!=nil{writeError(w,http.StatusUnauthorized,"unauthorized");return true}
 tenantID:=h.organizationTenant(r);parts:=strings.Split(strings.Trim(r.URL.Path,"/"),"/")
 if len(parts)==6&&parts[5]=="memberships"&&r.Method==http.MethodGet{
  items,e:=h.Store.ListPropertyMemberships(r.Context(),principal.IdentityID,tenantID,organizationID,propertyID);if e!=nil{writePropertyMembershipError(w,e);return true}
  writeJSON(w,http.StatusOK,map[string]any{"memberships":items});return true
 }
 if len(parts)==6&&parts[5]=="memberships"&&r.Method==http.MethodPost{
  var in PropertyMembershipInput;if err=decodeOrganizationInput(r,&in);err!=nil{writeError(w,http.StatusBadRequest,"invalid property membership payload");return true}
  item,e:=h.Store.AssignPropertyMembership(r.Context(),principal.IdentityID,tenantID,organizationID,propertyID,in);if e!=nil{writePropertyMembershipError(w,e);return true}
  writeJSON(w,http.StatusCreated,map[string]any{"membership":item});return true
 }
 if len(parts)==8&&parts[5]=="memberships"&&parts[7]=="role"&&r.Method==http.MethodPatch{
  var in struct{Role string `json:"role"`};if err=decodeOrganizationInput(r,&in);err!=nil{writeError(w,http.StatusBadRequest,"invalid property membership role");return true}
  item,e:=h.Store.ChangePropertyMembershipRole(r.Context(),principal.IdentityID,tenantID,organizationID,propertyID,parts[6],in.Role);if e!=nil{writePropertyMembershipError(w,e);return true}
  writeJSON(w,http.StatusOK,map[string]any{"membership":item});return true
 }
 if len(parts)==7&&parts[5]=="memberships"&&r.Method==http.MethodDelete{
  e:=h.Store.RevokePropertyMembership(r.Context(),principal.IdentityID,tenantID,organizationID,propertyID,parts[6]);if e!=nil{writePropertyMembershipError(w,e);return true}
  writeJSON(w,http.StatusOK,map[string]string{"status":"revoked"});return true
 }
 return false
}

func writePropertyMembershipError(w http.ResponseWriter,err error){
 switch{
 case errors.Is(err,ErrPropertyMembershipForbidden):writeError(w,http.StatusForbidden,"property membership operation forbidden")
 case errors.Is(err,ErrPropertyMembershipNotFound):writeError(w,http.StatusNotFound,"property or membership not found")
 case errors.Is(err,ErrPropertyMembershipConflict):writeError(w,http.StatusConflict,"property membership state conflict")
 case errors.Is(err,ErrOrganizationInput):writeError(w,http.StatusBadRequest,"invalid property membership payload")
 default:writeError(w,http.StatusInternalServerError,"property membership operation failed")
 }
}
