package marketplace

import (
 "context"
 "encoding/json"
 "errors"
 "fmt"
 "strings"
 "time"

 "github.com/jackc/pgx/v5"
)

var (
 ErrSupportCaseNotFound=errors.New("support case not found")
 ErrSupportCaseState=errors.New("invalid support case state")
 ErrSupportCaseInput=errors.New("invalid support case input")
)

type SupportCase struct {
 ID string `json:"id"`
 TenantID string `json:"tenant_id"`
 RequesterIdentityID string `json:"requester_identity_id"`
 Category string `json:"category"`
 Priority string `json:"priority"`
 Status string `json:"status"`
 Title string `json:"title"`
 Description string `json:"description"`
 AssignedSupportIdentityID *string `json:"assigned_support_identity_id,omitempty"`
 SLADueAt time.Time `json:"sla_due_at"`
 CreatedAt time.Time `json:"created_at"`
 UpdatedAt time.Time `json:"updated_at"`
 ClosedAt *time.Time `json:"closed_at,omitempty"`
}

func (s *Store) CreateSupportCase(ctx context.Context,actor Actor,category,priority,title,description string,slaDueAt time.Time)(SupportCase,error){
 if (actor.Role!=RoleCustomer&&actor.Role!=RoleProfessional)||actor.SubjectID==""||actor.TenantID==""{return SupportCase{},ErrForbidden}
 category=strings.TrimSpace(category);title=strings.TrimSpace(title);description=strings.TrimSpace(description)
 if len(category)<2||len(category)>80||len(title)<4||len(title)>180||len(description)>4000||
  (priority!="low"&&priority!="normal"&&priority!="high"&&priority!="urgent")||
  !slaDueAt.After(time.Now())||slaDueAt.After(time.Now().Add(30*24*time.Hour)){return SupportCase{},ErrSupportCaseInput}
 tx,err:=s.DB.BeginTx(ctx,pgx.TxOptions{});if err!=nil{return SupportCase{},err};defer tx.Rollback(ctx)
 id,err:=newID();if err!=nil{return SupportCase{},err}
 var result SupportCase
 err=tx.QueryRow(ctx,`
 INSERT INTO support_cases(id,tenant_id,requester_identity_id,category,priority,status,title,description,sla_due_at)
 VALUES($1,$2,$3,$4,$5,'open',$6,$7,$8)
 RETURNING id,tenant_id,requester_identity_id,category,priority,status,title,description,sla_due_at,created_at,updated_at
 `,id,actor.TenantID,actor.SubjectID,category,priority,title,description,slaDueAt.UTC()).Scan(
  &result.ID,&result.TenantID,&result.RequesterIdentityID,&result.Category,&result.Priority,&result.Status,&result.Title,&result.Description,&result.SLADueAt,&result.CreatedAt,&result.UpdatedAt)
 if err!=nil{return SupportCase{},err}
 before:=json.RawMessage(`{}`);after,_:=json.Marshal(map[string]any{"status":result.Status,"priority":result.Priority,"category":result.Category})
 if _,err=tx.Exec(ctx,`INSERT INTO support_case_events(case_id,actor_identity_id,action,reason,before_state,after_state) VALUES($1,$2,'created','case opened',$3::jsonb,$4::jsonb)`,id,actor.SubjectID,[]byte(before),after);err!=nil{return SupportCase{},err}
 if err=tx.Commit(ctx);err!=nil{return SupportCase{},err}
 return result,nil
}

func (s *Store) GetSupportCase(ctx context.Context,actor Actor,caseID string)(SupportCase,error){
 var result SupportCase
 err:=s.DB.QueryRow(ctx,`
 SELECT id,tenant_id,requester_identity_id,category,priority,status,title,description,
 assigned_support_identity_id,sla_due_at,created_at,updated_at,closed_at
 FROM support_cases WHERE id=$1
 `,caseID).Scan(&result.ID,&result.TenantID,&result.RequesterIdentityID,&result.Category,&result.Priority,&result.Status,&result.Title,&result.Description,
  &result.AssignedSupportIdentityID,&result.SLADueAt,&result.CreatedAt,&result.UpdatedAt,&result.ClosedAt)
 if errors.Is(err,pgx.ErrNoRows){return SupportCase{},ErrSupportCaseNotFound};if err!=nil{return SupportCase{},err}
 if actor.SubjectID==""||actor.TenantID!=result.TenantID{return SupportCase{},ErrForbidden}
 if actor.Role==RoleSupport{
  if err=Authorize(actor,Resource{TenantID:result.TenantID},ActionSupportCaseRead);err!=nil{return SupportCase{},err}
  var active bool
  if err=s.DB.QueryRow(ctx,`SELECT EXISTS(SELECT 1 FROM support_agents WHERE tenant_id=$1 AND identity_id=$2 AND active=true)`,actor.TenantID,actor.SubjectID).Scan(&active);err!=nil{return SupportCase{},err}
  if !active{return SupportCase{},ErrForbidden}
 }else if (actor.Role!=RoleCustomer&&actor.Role!=RoleProfessional)||actor.SubjectID!=result.RequesterIdentityID{return SupportCase{},ErrForbidden}
 return result,nil
}

func (s *Store) AssignSupportCase(ctx context.Context,actor Actor,caseID,assigneeID,reason string)(SupportCase,error){
 if actor.Role!=RoleSupport||actor.SubjectID==""||actor.TenantID==""||strings.TrimSpace(assigneeID)==""{return SupportCase{},ErrForbidden}
 reason=strings.TrimSpace(reason);if reason==""||len(reason)>500{return SupportCase{},ErrSupportCaseInput}
 tx,err:=s.DB.BeginTx(ctx,pgx.TxOptions{});if err!=nil{return SupportCase{},err};defer tx.Rollback(ctx)
 var result SupportCase
 err=tx.QueryRow(ctx,`
 SELECT id,tenant_id,requester_identity_id,category,priority,status,title,description,assigned_support_identity_id,sla_due_at,created_at,updated_at,closed_at
 FROM support_cases WHERE id=$1 FOR UPDATE
 `,caseID).Scan(&result.ID,&result.TenantID,&result.RequesterIdentityID,&result.Category,&result.Priority,&result.Status,&result.Title,&result.Description,&result.AssignedSupportIdentityID,&result.SLADueAt,&result.CreatedAt,&result.UpdatedAt,&result.ClosedAt)
 if errors.Is(err,pgx.ErrNoRows){return SupportCase{},ErrSupportCaseNotFound};if err!=nil{return SupportCase{},err}
 if result.TenantID!=actor.TenantID{return SupportCase{},ErrForbidden}
 if err=Authorize(actor,Resource{TenantID:result.TenantID},ActionSupportCaseComment);err!=nil{return SupportCase{},err}
 var actorActive bool
 if err=tx.QueryRow(ctx,`SELECT EXISTS(SELECT 1 FROM support_agents WHERE tenant_id=$1 AND identity_id=$2 AND active=true)`,actor.TenantID,actor.SubjectID).Scan(&actorActive);err!=nil{return SupportCase{},err}
 if !actorActive{return SupportCase{},ErrForbidden}
 var active bool
 if err=tx.QueryRow(ctx,`SELECT EXISTS(SELECT 1 FROM support_agents WHERE tenant_id=$1 AND identity_id=$2 AND active=true)`,actor.TenantID,assigneeID).Scan(&active);err!=nil{return SupportCase{},err}
 if !active{return SupportCase{},ErrForbidden}
 before,_:=json.Marshal(map[string]any{"assigned_support_identity_id":result.AssignedSupportIdentityID})
 _,err=tx.Exec(ctx,`UPDATE support_cases SET assigned_support_identity_id=$2,updated_at=now() WHERE id=$1`,caseID,assigneeID);if err!=nil{return SupportCase{},err}
 result.AssignedSupportIdentityID=&assigneeID
 after,_:=json.Marshal(map[string]any{"assigned_support_identity_id":assigneeID})
 if _,err=tx.Exec(ctx,`INSERT INTO support_case_events(case_id,actor_identity_id,action,reason,before_state,after_state) VALUES($1,$2,'assigned',$3,$4::jsonb,$5::jsonb)`,caseID,actor.SubjectID,reason,before,after);err!=nil{return SupportCase{},err}
 if err=tx.Commit(ctx);err!=nil{return SupportCase{},err}
 return result,nil
}

func (s *Store) TransitionSupportCase(ctx context.Context,actor Actor,caseID,target,reason string)(SupportCase,error){
 if actor.Role!=RoleSupport||actor.SubjectID==""||actor.TenantID==""{return SupportCase{},ErrForbidden}
 reason=strings.TrimSpace(reason);if reason==""||len(reason)>500{return SupportCase{},ErrSupportCaseInput}
 tx,err:=s.DB.BeginTx(ctx,pgx.TxOptions{});if err!=nil{return SupportCase{},err};defer tx.Rollback(ctx)
 var result SupportCase
 err=tx.QueryRow(ctx,`SELECT id,tenant_id,requester_identity_id,category,priority,status,title,description,assigned_support_identity_id,sla_due_at,created_at,updated_at,closed_at FROM support_cases WHERE id=$1 FOR UPDATE`,caseID).
  Scan(&result.ID,&result.TenantID,&result.RequesterIdentityID,&result.Category,&result.Priority,&result.Status,&result.Title,&result.Description,&result.AssignedSupportIdentityID,&result.SLADueAt,&result.CreatedAt,&result.UpdatedAt,&result.ClosedAt)
 if errors.Is(err,pgx.ErrNoRows){return SupportCase{},ErrSupportCaseNotFound};if err!=nil{return SupportCase{},err}
 if result.TenantID!=actor.TenantID{return SupportCase{},ErrForbidden}
 if err=Authorize(actor,Resource{TenantID:result.TenantID},ActionSupportCaseComment);err!=nil{return SupportCase{},err}
 var actorActive bool
 if err=tx.QueryRow(ctx,`SELECT EXISTS(SELECT 1 FROM support_agents WHERE tenant_id=$1 AND identity_id=$2 AND active=true)`,actor.TenantID,actor.SubjectID).Scan(&actorActive);err!=nil{return SupportCase{},err}
 if !actorActive{return SupportCase{},ErrForbidden}
 allowed:=false
 switch result.Status{
 case "open":allowed=target=="in_progress"
 case "in_progress":allowed=target=="waiting_customer"||target=="resolved"
 case "waiting_customer":allowed=target=="in_progress"||target=="resolved"
 case "resolved":allowed=target=="closed"
 }
 if !allowed{return SupportCase{},ErrSupportCaseState}
 before,_:=json.Marshal(map[string]any{"status":result.Status})
 var closedAt *time.Time
 err=tx.QueryRow(ctx,`UPDATE support_cases SET status=$2,updated_at=now(),closed_at=CASE WHEN $2='closed' THEN now() ELSE closed_at END WHERE id=$1 RETURNING updated_at,closed_at`,caseID,target).Scan(&result.UpdatedAt,&closedAt)
 if err!=nil{return SupportCase{},err}
 previous:=result.Status;result.Status=target;result.ClosedAt=closedAt
 after,_:=json.Marshal(map[string]any{"status":target})
 if _,err=tx.Exec(ctx,`INSERT INTO support_case_events(case_id,actor_identity_id,action,reason,before_state,after_state) VALUES($1,$2,'status_changed',$3,$4::jsonb,$5::jsonb)`,caseID,actor.SubjectID,reason,before,after);err!=nil{return SupportCase{},err}
 if err=tx.Commit(ctx);err!=nil{return SupportCase{},err}
 _=previous
 return result,nil
}

func (c SupportCase) String()string{return fmt.Sprintf("support case %s (%s)",c.ID,c.Status)}
