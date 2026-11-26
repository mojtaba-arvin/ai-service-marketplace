package marketplace

import (
 "context"
 "errors"
 "fmt"
 "strings"
 "time"

 "github.com/jackc/pgx/v5"
)

var (
 ErrVerificationCaseNotFound=errors.New("verification case not found")
 ErrVerificationState=errors.New("invalid verification case state")
 ErrVerificationEvidence=errors.New("invalid private evidence reference")
 ErrVerificationAlreadyExists=errors.New("provider already has an open verification case")
)

type ProfessionalVerificationCase struct {
 ID string `json:"id"`
 ProviderID string `json:"provider_id"`
 SubjectIdentityID string `json:"subject_identity_id"`
 Status string `json:"status"`
 EvidenceRef string `json:"evidence_ref,omitempty"`
 ReviewerIdentityID *string `json:"reviewer_identity_id,omitempty"`
 DecisionReason string `json:"decision_reason,omitempty"`
 CreatedAt time.Time `json:"created_at"`
 UpdatedAt time.Time `json:"updated_at"`
 DecidedAt *time.Time `json:"decided_at,omitempty"`
}

func validPrivateEvidenceRef(ref string)bool{
 if len(ref)<12||len(ref)>512||strings.TrimSpace(ref)!=ref||strings.ContainsAny(ref,"?#\r\n") {return false}
 return strings.HasPrefix(ref,"private://")&&len(strings.TrimPrefix(ref,"private://"))>=8
}

func (s *Store) CreateProfessionalVerificationCase(ctx context.Context,actor Actor,providerID,evidenceRef string)(ProfessionalVerificationCase,error){
 if actor.Role!=RoleProfessional||actor.SubjectID==""||actor.TenantID=="" {return ProfessionalVerificationCase{},ErrForbidden}
 if !validPrivateEvidenceRef(evidenceRef){return ProfessionalVerificationCase{},ErrVerificationEvidence}
 tx,err:=s.DB.BeginTx(ctx,pgx.TxOptions{});if err!=nil{return ProfessionalVerificationCase{},err};defer tx.Rollback(ctx)
 var subjectID,tenantID string
 err=tx.QueryRow(ctx,`SELECT identity_id,tenant_id FROM provider_profiles WHERE id=$1 FOR UPDATE`,providerID).Scan(&subjectID,&tenantID)
 if errors.Is(err,pgx.ErrNoRows){return ProfessionalVerificationCase{},ErrVerificationCaseNotFound};if err!=nil{return ProfessionalVerificationCase{},err}
 if subjectID!=actor.SubjectID||tenantID!=actor.TenantID{return ProfessionalVerificationCase{},ErrForbidden}
 var verifiedAt *time.Time
 if err=tx.QueryRow(ctx,`SELECT verified_at FROM provider_profiles WHERE id=$1`,providerID).Scan(&verifiedAt);err!=nil{return ProfessionalVerificationCase{},err}
 if verifiedAt!=nil{return ProfessionalVerificationCase{},ErrVerificationState}
 var existing bool
 if err=tx.QueryRow(ctx,`SELECT EXISTS(SELECT 1 FROM professional_verification_cases WHERE provider_id=$1 AND status IN ('submitted','in_review'))`,providerID).Scan(&existing);err!=nil{return ProfessionalVerificationCase{},err}
 if existing{return ProfessionalVerificationCase{},ErrVerificationAlreadyExists}
 id,err:=newID();if err!=nil{return ProfessionalVerificationCase{},err}
 var result ProfessionalVerificationCase
 err=tx.QueryRow(ctx,`
 INSERT INTO professional_verification_cases(id,provider_id,subject_identity_id,status,evidence_ref)
 VALUES($1,$2,$3,'submitted',$4)
 RETURNING id,provider_id,subject_identity_id,status,evidence_ref,created_at,updated_at
 `,id,providerID,subjectID,evidenceRef).Scan(&result.ID,&result.ProviderID,&result.SubjectIdentityID,&result.Status,&result.EvidenceRef,&result.CreatedAt,&result.UpdatedAt)
 if err!=nil{return ProfessionalVerificationCase{},err}
 if _,err=tx.Exec(ctx,`INSERT INTO professional_verification_events(case_id,actor_identity_id,action,to_status,evidence_ref) VALUES($1,$2,'submitted','submitted',$3)`,id,actor.SubjectID,evidenceRef);err!=nil{return ProfessionalVerificationCase{},err}
 if err=tx.Commit(ctx);err!=nil{return ProfessionalVerificationCase{},err}
 return result,nil
}


func (s *Store) StartProfessionalVerificationReview(ctx context.Context, actor Actor, caseID, reason string) (ProfessionalVerificationCase, error) {
	if actor.Role != RoleOperator || actor.SubjectID == "" || actor.TenantID == "" {
		return ProfessionalVerificationCase{}, ErrForbidden
	}
	reason = strings.TrimSpace(reason)
	if reason == "" || len(reason) > 1000 {
		return ProfessionalVerificationCase{}, ErrVerificationState
	}
	tx, err := s.DB.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return ProfessionalVerificationCase{}, err
	}
	defer tx.Rollback(ctx)

	var result ProfessionalVerificationCase
	var tenantID string
	err = tx.QueryRow(ctx, `
		SELECT c.id,c.provider_id,c.subject_identity_id,c.status,c.evidence_ref,
			c.created_at,c.updated_at,p.tenant_id
		FROM professional_verification_cases c
		JOIN provider_profiles p ON p.id=c.provider_id
		WHERE c.id=$1
		FOR UPDATE OF c,p
	`, caseID).Scan(
		&result.ID, &result.ProviderID, &result.SubjectIdentityID, &result.Status,
		&result.EvidenceRef, &result.CreatedAt, &result.UpdatedAt, &tenantID,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return ProfessionalVerificationCase{}, ErrVerificationCaseNotFound
	}
	if err != nil {
		return ProfessionalVerificationCase{}, err
	}
	if tenantID != actor.TenantID || result.SubjectIdentityID == actor.SubjectID {
		return ProfessionalVerificationCase{}, ErrForbidden
	}
	if err = authorizeOperatorTx(ctx, tx, actor, Resource{TenantID: tenantID}, ActionProviderVerify); err != nil {
		return ProfessionalVerificationCase{}, err
	}
	if result.Status != "submitted" {
		return ProfessionalVerificationCase{}, ErrVerificationState
	}
	_, err = tx.Exec(ctx, `
		UPDATE professional_verification_cases SET status='in_review',updated_at=now()
		WHERE id=$1 AND status='submitted'
	`, caseID)
	if err != nil {
		return ProfessionalVerificationCase{}, err
	}
	if _, err = tx.Exec(ctx, `
		INSERT INTO professional_verification_events(
			case_id,actor_identity_id,action,from_status,to_status,reason,evidence_ref
		) VALUES($1,$2,'reviewed','submitted','in_review',$3,$4)
	`, caseID, actor.SubjectID, reason, result.EvidenceRef); err != nil {
		return ProfessionalVerificationCase{}, err
	}
	if err = tx.QueryRow(ctx, `
		SELECT status,updated_at FROM professional_verification_cases WHERE id=$1
	`, caseID).Scan(&result.Status, &result.UpdatedAt); err != nil {
		return ProfessionalVerificationCase{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return ProfessionalVerificationCase{}, err
	}
	return result, nil
}

func (s *Store) ReviewProfessionalVerificationCase(ctx context.Context,actor Actor,caseID,decision,reason string)(ProfessionalVerificationCase,error){
 if actor.Role!=RoleOperator||actor.SubjectID==""||actor.TenantID=="" {return ProfessionalVerificationCase{},ErrForbidden}
 if decision!="approved"&&decision!="rejected" {return ProfessionalVerificationCase{},ErrVerificationState}
 reason=strings.TrimSpace(reason);if reason==""||len(reason)>1000{return ProfessionalVerificationCase{},ErrVerificationState}
 tx,err:=s.DB.BeginTx(ctx,pgx.TxOptions{});if err!=nil{return ProfessionalVerificationCase{},err};defer tx.Rollback(ctx)
 var result ProfessionalVerificationCase
 var tenantID string
 err=tx.QueryRow(ctx,`
 SELECT c.id,c.provider_id,c.subject_identity_id,c.status,c.evidence_ref,c.created_at,c.updated_at,p.tenant_id
 FROM professional_verification_cases c JOIN provider_profiles p ON p.id=c.provider_id
 WHERE c.id=$1 FOR UPDATE OF c,p
 `,caseID).Scan(&result.ID,&result.ProviderID,&result.SubjectIdentityID,&result.Status,&result.EvidenceRef,&result.CreatedAt,&result.UpdatedAt,&tenantID)
 if errors.Is(err,pgx.ErrNoRows){return ProfessionalVerificationCase{},ErrVerificationCaseNotFound};if err!=nil{return ProfessionalVerificationCase{},err}
 if tenantID!=actor.TenantID||result.SubjectIdentityID==actor.SubjectID{return ProfessionalVerificationCase{},ErrForbidden}
 if err=authorizeOperatorTx(ctx,tx,actor,Resource{TenantID:tenantID},ActionProviderVerify);err!=nil{return ProfessionalVerificationCase{},err}
 if result.Status!="submitted"&&result.Status!="in_review"{return ProfessionalVerificationCase{},ErrVerificationState}
 fromStatus := result.Status
 var decided time.Time
 err=tx.QueryRow(ctx,`
 UPDATE professional_verification_cases SET status=$2,reviewer_identity_id=$3,decision_reason=$4,decided_at=now(),updated_at=now()
 WHERE id=$1 RETURNING status,reviewer_identity_id,decision_reason,decided_at,updated_at
 `,caseID,decision,actor.SubjectID,reason).Scan(&result.Status,&result.ReviewerIdentityID,&result.DecisionReason,&decided,&result.UpdatedAt)
 if err!=nil{return ProfessionalVerificationCase{},err}
 result.DecidedAt=&decided
 if decision=="approved"{
  _,err=tx.Exec(ctx,`UPDATE provider_profiles SET verified_at=now(),verified_by_identity_id=$2,updated_at=now() WHERE id=$1 AND verified_at IS NULL`,result.ProviderID,actor.SubjectID)
  if err!=nil{return ProfessionalVerificationCase{},err}
 } else {
  _,err=tx.Exec(ctx,`UPDATE provider_profiles SET verified_at=NULL,verified_by_identity_id=NULL,updated_at=now() WHERE id=$1`,result.ProviderID)
  if err!=nil{return ProfessionalVerificationCase{},err}
 }
 if _,err=tx.Exec(ctx,`INSERT INTO professional_verification_events(case_id,actor_identity_id,action,from_status,to_status,reason,evidence_ref) VALUES($1,$2,'reviewed',$3,$4,$5,$6)`,caseID,actor.SubjectID,fromStatus,decision,reason,result.EvidenceRef);err!=nil{return ProfessionalVerificationCase{},err}
 if err=tx.Commit(ctx);err!=nil{return ProfessionalVerificationCase{},err}
 return result,nil
}

// GetProfessionalVerificationCase hides the private evidence pointer from the
// subject. Only same-tenant operators may retrieve that pointer.
func (s *Store) GetProfessionalVerificationCase(ctx context.Context, actor Actor, caseID string) (ProfessionalVerificationCase, error) {
	tx, err := s.DB.BeginTx(ctx, pgx.TxOptions{})
	if err != nil { return ProfessionalVerificationCase{}, err }
	defer tx.Rollback(ctx)
	var result ProfessionalVerificationCase
	var tenantID string
	err = tx.QueryRow(ctx, `
		SELECT c.id,c.provider_id,c.subject_identity_id,c.status,c.evidence_ref,c.reviewer_identity_id,c.decision_reason,c.created_at,c.updated_at,c.decided_at,p.tenant_id
		FROM professional_verification_cases c JOIN provider_profiles p ON p.id=c.provider_id WHERE c.id=$1
	`, caseID).Scan(&result.ID,&result.ProviderID,&result.SubjectIdentityID,&result.Status,&result.EvidenceRef,
		&result.ReviewerIdentityID,&result.DecisionReason,&result.CreatedAt,&result.UpdatedAt,&result.DecidedAt,&tenantID)
	if errors.Is(err, pgx.ErrNoRows) { return ProfessionalVerificationCase{}, ErrVerificationCaseNotFound }
	if err != nil { return ProfessionalVerificationCase{}, err }
	if actor.TenantID != tenantID || actor.SubjectID == "" { return ProfessionalVerificationCase{}, ErrForbidden }
	switch actor.Role {
	case RoleProfessional:
		if actor.SubjectID != result.SubjectIdentityID { return ProfessionalVerificationCase{}, ErrForbidden }
		result.EvidenceRef = ""
	case RoleOperator:
		if err = authorizeOperatorTx(ctx, tx, actor, Resource{TenantID: tenantID}, ActionProviderVerify); err != nil {
			return ProfessionalVerificationCase{}, err
		}
	default:
		return ProfessionalVerificationCase{}, ErrForbidden
	}
	if err = tx.Commit(ctx); err != nil { return ProfessionalVerificationCase{}, err }
	return result, nil
}

func (c ProfessionalVerificationCase) String()string{return fmt.Sprintf("verification case %s (%s)",c.ID,c.Status)}
