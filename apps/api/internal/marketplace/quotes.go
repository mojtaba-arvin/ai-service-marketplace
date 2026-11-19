package marketplace

import (
	"context"
	"errors"
	"fmt"
	"math"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

var (
	ErrInvalidQuote = errors.New("invalid inspection quote")
	ErrQuoteOrderNotReady = errors.New("order is not eligible for an inspection quote")
	ErrQuoteStateConflict = errors.New("invalid quote decision transition")
	ErrQuoteNotFound = errors.New("inspection quote not found")
)

var quoteLineCodePattern=regexp.MustCompile(`^[a-z][a-z0-9_-]{1,63}$`)

type QuoteLineInput struct {
	Code string
	Description string
	Quantity int
	UnitAmountMinor int64
}

type QuoteEvidenceInput struct {
	Kind string
	Reference string
}

type QuoteLine struct {
	ID string `json:"id"`
	Code string `json:"code"`
	Description string `json:"description"`
	Quantity int `json:"quantity"`
	UnitAmountMinor int64 `json:"unit_amount_minor"`
	LineTotalMinor int64 `json:"line_total_minor"`
}

type QuoteEvidence struct {
	Kind string `json:"kind"`
	Reference string `json:"reference"`
}

type InspectionQuote struct {
	ID string `json:"id"`
	OrderID string `json:"order_id"`
	ProviderID string `json:"provider_id"`
	RevisionNo int `json:"revision_no"`
	Currency string `json:"currency"`
	Scope string `json:"scope"`
	TotalMinor int64 `json:"total_minor"`
	ExpiresAt time.Time `json:"expires_at"`
	CreatedAt time.Time `json:"created_at"`
	Lines []QuoteLine `json:"lines,omitempty"`
	Evidence []QuoteEvidence `json:"evidence,omitempty"`
}

type QuoteDecision struct {
	ID string `json:"id"`
	QuoteID string `json:"quote_id"`
	ActorIdentityID string `json:"actor_identity_id"`
	ActorRole string `json:"actor_role"`
	Action string `json:"action"`
	Reason string `json:"reason"`
	CreatedAt time.Time `json:"created_at"`
}

func (s *Store) CreateQuote(ctx context.Context,actor Actor,orderID,scope string,expiresAt time.Time,
	lines []QuoteLineInput,evidence []QuoteEvidenceInput) (InspectionQuote,error) {
	scope=strings.TrimSpace(scope)
	if actor.Role!=RoleProfessional || actor.SubjectID=="" || actor.TenantID=="" ||
		orderID=="" || scope=="" || len(scope)>4000 || len(lines)==0 || len(lines)>50 || len(evidence)>10 {
		return InspectionQuote{},ErrInvalidQuote
	}
	now:=time.Now().UTC()
	if expiresAt.Before(now.Add(time.Minute)) || expiresAt.After(now.Add(7*24*time.Hour)) {
		return InspectionQuote{},ErrInvalidQuote
	}
	seenCodes:=make(map[string]bool,len(lines))
	var total int64
	for i:=range lines {
		lines[i].Code=strings.TrimSpace(lines[i].Code)
		lines[i].Description=strings.TrimSpace(lines[i].Description)
		if !quoteLineCodePattern.MatchString(lines[i].Code) || seenCodes[lines[i].Code] ||
			lines[i].Description=="" || len(lines[i].Description)>500 ||
			lines[i].Quantity<1 || lines[i].Quantity>10000 || lines[i].UnitAmountMinor<=0 ||
			lines[i].UnitAmountMinor>math.MaxInt64/int64(lines[i].Quantity) {
			return InspectionQuote{},ErrInvalidQuote
		}
		seenCodes[lines[i].Code]=true
		lineTotal:=lines[i].UnitAmountMinor*int64(lines[i].Quantity)
		if total>math.MaxInt64-lineTotal { return InspectionQuote{},ErrInvalidQuote }
		total+=lineTotal
	}
	if total<=0 { return InspectionQuote{},ErrInvalidQuote }
	for i:=range evidence {
		evidence[i].Kind=strings.TrimSpace(evidence[i].Kind)
		evidence[i].Reference=strings.TrimSpace(evidence[i].Reference)
		if (evidence[i].Kind!="photo" && evidence[i].Kind!="document" && evidence[i].Kind!="measurement" && evidence[i].Kind!="other") ||
			!strings.HasPrefix(evidence[i].Reference,"evidence://") || len(evidence[i].Reference)>500 {
			return InspectionQuote{},ErrInvalidQuote
		}
	}

	tx,err:=s.DB.BeginTx(ctx,pgx.TxOptions{})
	if err!=nil { return InspectionQuote{},err }
	defer tx.Rollback(ctx)
	var customerID,currency,status string
	var priceVersionID *string
	err=tx.QueryRow(ctx,`
		SELECT customer_id,currency,status,price_version_id FROM orders WHERE id=$1 FOR UPDATE
	`,orderID).Scan(&customerID,&currency,&status,&priceVersionID)
	if errors.Is(err,pgx.ErrNoRows) { return InspectionQuote{},ErrQuoteOrderNotReady }
	if err!=nil { return InspectionQuote{},err }
	if (status!="accepted" && status!="in_progress") || priceVersionID==nil {
		return InspectionQuote{},ErrQuoteOrderNotReady
	}
	var categoryID string
	err=tx.QueryRow(ctx,`
		SELECT m.category_id FROM service_price_versions v
		JOIN service_price_menus m ON m.id=v.menu_id WHERE v.id=$1
	`,*priceVersionID).Scan(&categoryID)
	if err!=nil { return InspectionQuote{},err }
	var providerID,providerTenant string
	err=tx.QueryRow(ctx,`
		SELECT id,tenant_id FROM provider_profiles
		WHERE identity_id=$1 AND active=true AND verified_at IS NOT NULL FOR SHARE
	`,actor.SubjectID).Scan(&providerID,&providerTenant)
	if errors.Is(err,pgx.ErrNoRows) { return InspectionQuote{},ErrProviderNotEligible }
	if err!=nil { return InspectionQuote{},err }
	if err=Authorize(actor,Resource{TenantID:providerTenant,OwnerID:actor.SubjectID},ActionProfileUpdate); err!=nil {
		return InspectionQuote{},err
	}
	var hasVerifiedSkill bool
	err=tx.QueryRow(ctx,`SELECT EXISTS(SELECT 1 FROM provider_skills WHERE provider_id=$1 AND category_id=$2 AND verified_at IS NOT NULL)`,
		providerID,categoryID).Scan(&hasVerifiedSkill)
	if err!=nil { return InspectionQuote{},err }
	if !hasVerifiedSkill { return InspectionQuote{},ErrProviderNotEligible }
	var activeQuoteExists bool
	err=tx.QueryRow(ctx,`
		SELECT EXISTS(
			SELECT 1 FROM inspection_quotes q
			WHERE q.order_id=$1 AND NOT EXISTS(
				SELECT 1 FROM quote_decisions d WHERE d.quote_id=q.id AND d.action='rejected'
			)
		)
	`,orderID).Scan(&activeQuoteExists)
	if err!=nil { return InspectionQuote{},err }
	if activeQuoteExists { return InspectionQuote{},ErrQuoteStateConflict }

	var revision int
	if err=tx.QueryRow(ctx,`SELECT COALESCE(MAX(revision_no),0)+1 FROM inspection_quotes WHERE order_id=$1`,orderID).Scan(&revision); err!=nil {
		return InspectionQuote{},err
	}
	id,err:=newID()
	if err!=nil { return InspectionQuote{},err }
	var quote InspectionQuote
	err=tx.QueryRow(ctx,`
		INSERT INTO inspection_quotes(id,order_id,provider_id,revision_no,currency,scope,total_minor,expires_at,created_by_identity_id)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)
		RETURNING id,order_id,provider_id,revision_no,currency,scope,total_minor,expires_at,created_at
	`,id,orderID,providerID,revision,currency,scope,total,expiresAt,actor.SubjectID).Scan(&quote.ID,&quote.OrderID,
		&quote.ProviderID,&quote.RevisionNo,&quote.Currency,&quote.Scope,&quote.TotalMinor,&quote.ExpiresAt,&quote.CreatedAt)
	if err!=nil { return InspectionQuote{},fmt.Errorf("create inspection quote: %w",err) }
	quote.Lines=make([]QuoteLine,0,len(lines))
	for _,line:=range lines {
		lineID,genErr:=newID()
		if genErr!=nil { return InspectionQuote{},genErr }
		lineTotal:=line.UnitAmountMinor*int64(line.Quantity)
		var saved QuoteLine
		err=tx.QueryRow(ctx,`
			INSERT INTO quote_line_items(id,quote_id,line_code,description,quantity,unit_amount_minor,line_total_minor)
			VALUES($1,$2,$3,$4,$5,$6,$7)
			RETURNING id,line_code,description,quantity,unit_amount_minor,line_total_minor
		`,lineID,id,line.Code,line.Description,line.Quantity,line.UnitAmountMinor,lineTotal).Scan(&saved.ID,&saved.Code,
			&saved.Description,&saved.Quantity,&saved.UnitAmountMinor,&saved.LineTotalMinor)
		if err!=nil { return InspectionQuote{},err }
		quote.Lines=append(quote.Lines,saved)
	}
	quote.Evidence=make([]QuoteEvidence,0,len(evidence))
	for _,item:=range evidence {
		evidenceID,genErr:=newID()
		if genErr!=nil { return InspectionQuote{},genErr }
		_,err=tx.Exec(ctx,`
			INSERT INTO quote_evidence_refs(id,quote_id,evidence_kind,evidence_reference) VALUES($1,$2,$3,$4)
		`,evidenceID,id,item.Kind,item.Reference)
		if err!=nil { return InspectionQuote{},err }
		quote.Evidence=append(quote.Evidence,QuoteEvidence{Kind:item.Kind,Reference:item.Reference})
	}
	if err=tx.Commit(ctx); err!=nil { return InspectionQuote{},err }
	return quote,nil
}

func latestQuoteAction(ctx context.Context,tx pgx.Tx,quoteID string) (string,error) {
	var action string
	err:=tx.QueryRow(ctx,`SELECT action FROM quote_decisions WHERE quote_id=$1 ORDER BY created_at DESC,id DESC LIMIT 1`,quoteID).Scan(&action)
	if errors.Is(err,pgx.ErrNoRows) { return "",nil }
	return action,err
}

func (s *Store) appendQuoteDecision(ctx context.Context,tx pgx.Tx,quoteID string,actor Actor,action,reason string) (QuoteDecision,error) {
	id,err:=newID()
	if err!=nil { return QuoteDecision{},err }
	var decision QuoteDecision
	err=tx.QueryRow(ctx,`
		INSERT INTO quote_decisions(id,quote_id,actor_identity_id,actor_role,action,reason)
		VALUES($1,$2,$3,$4,$5,$6)
		RETURNING id,quote_id,actor_identity_id,actor_role,action,reason,created_at
	`,id,quoteID,actor.SubjectID,string(actor.Role),action,reason).Scan(&decision.ID,&decision.QuoteID,
		&decision.ActorIdentityID,&decision.ActorRole,&decision.Action,&decision.Reason,&decision.CreatedAt)
	return decision,err
}

func (s *Store) ApproveQuote(ctx context.Context,actor Actor,quoteID string) (QuoteDecision,error) {
	if actor.Role!=RoleCustomer || actor.SubjectID=="" || actor.TenantID=="" { return QuoteDecision{},ErrForbidden }
	if err:=Authorize(actor,Resource{TenantID:actor.TenantID,OwnerID:actor.SubjectID},ActionQuoteApprove); err!=nil { return QuoteDecision{},err }
	tx,err:=s.DB.BeginTx(ctx,pgx.TxOptions{})
	if err!=nil { return QuoteDecision{},err }
	defer tx.Rollback(ctx)
	var customerID string
	var expiresAt time.Time
	err=tx.QueryRow(ctx,`
		SELECT o.customer_id,q.expires_at FROM inspection_quotes q JOIN orders o ON o.id=q.order_id
		WHERE q.id=$1 FOR UPDATE OF q
	`,quoteID).Scan(&customerID,&expiresAt)
	if errors.Is(err,pgx.ErrNoRows) { return QuoteDecision{},ErrQuoteNotFound }
	if err!=nil { return QuoteDecision{},err }
	if customerID!=actor.SubjectID { return QuoteDecision{},ErrForbidden }
	if !time.Now().Before(expiresAt) { return QuoteDecision{},ErrQuoteStateConflict }
	last,err:=latestQuoteAction(ctx,tx,quoteID)
	if err!=nil { return QuoteDecision{},err }
	if last!="" { return QuoteDecision{},ErrQuoteStateConflict }
	decision,err:=s.appendQuoteDecision(ctx,tx,quoteID,actor,"approved","")
	if err!=nil { return QuoteDecision{},err }
	if err=tx.Commit(ctx); err!=nil { return QuoteDecision{},err }
	return decision,nil
}

func (s *Store) RejectQuote(ctx context.Context,actor Actor,quoteID,reason string) (QuoteDecision,error) {
	reason=strings.TrimSpace(reason)
	if actor.Role!=RoleCustomer || actor.SubjectID=="" || reason=="" || len(reason)>1000 { return QuoteDecision{},ErrForbidden }
	if err:=Authorize(actor,Resource{TenantID:actor.TenantID,OwnerID:actor.SubjectID},ActionQuoteApprove); err!=nil { return QuoteDecision{},err }
	tx,err:=s.DB.BeginTx(ctx,pgx.TxOptions{})
	if err!=nil { return QuoteDecision{},err }
	defer tx.Rollback(ctx)
	var customerID string
	var expiresAt time.Time
	err=tx.QueryRow(ctx,`
		SELECT o.customer_id,q.expires_at FROM inspection_quotes q JOIN orders o ON o.id=q.order_id
		WHERE q.id=$1 FOR UPDATE OF q
	`,quoteID).Scan(&customerID,&expiresAt)
	if errors.Is(err,pgx.ErrNoRows) { return QuoteDecision{},ErrQuoteNotFound }
	if err!=nil { return QuoteDecision{},err }
	if customerID!=actor.SubjectID { return QuoteDecision{},ErrForbidden }
	if !time.Now().Before(expiresAt) { return QuoteDecision{},ErrQuoteStateConflict }
	last,err:=latestQuoteAction(ctx,tx,quoteID)
	if err!=nil { return QuoteDecision{},err }
	if last!="" { return QuoteDecision{},ErrQuoteStateConflict }
	decision,err:=s.appendQuoteDecision(ctx,tx,quoteID,actor,"rejected",reason)
	if err!=nil { return QuoteDecision{},err }
	if err=tx.Commit(ctx); err!=nil { return QuoteDecision{},err }
	return decision,nil
}

func (s *Store) AcceptQuote(ctx context.Context,actor Actor,quoteID string) (QuoteDecision,error) {
	if actor.Role!=RoleProfessional || actor.SubjectID=="" || actor.TenantID=="" { return QuoteDecision{},ErrForbidden }
	tx,err:=s.DB.BeginTx(ctx,pgx.TxOptions{})
	if err!=nil { return QuoteDecision{},err }
	defer tx.Rollback(ctx)
	var providerIdentityID,providerTenant,orderID,customerID string
	var expiresAt time.Time
	var quoteTotal,currentAmount int64
	err=tx.QueryRow(ctx,`
		SELECT p.identity_id,p.tenant_id,q.expires_at,q.order_id,q.total_minor,o.customer_id,o.amount_minor
		FROM inspection_quotes q
		JOIN provider_profiles p ON p.id=q.provider_id
		JOIN orders o ON o.id=q.order_id
		WHERE q.id=$1 FOR UPDATE OF q,o
	`,quoteID).Scan(&providerIdentityID,&providerTenant,&expiresAt,&orderID,&quoteTotal,&customerID,&currentAmount)
	if errors.Is(err,pgx.ErrNoRows) { return QuoteDecision{},ErrQuoteNotFound }
	if err!=nil { return QuoteDecision{},err }
	if providerIdentityID!=actor.SubjectID { return QuoteDecision{},ErrForbidden }
	if err=Authorize(actor,Resource{TenantID:providerTenant,OwnerID:providerIdentityID},ActionQuoteAccept); err!=nil { return QuoteDecision{},err }
	if !time.Now().Before(expiresAt) { return QuoteDecision{},ErrQuoteStateConflict }
	last,err:=latestQuoteAction(ctx,tx,quoteID)
	if err!=nil { return QuoteDecision{},err }
	if last!="approved" { return QuoteDecision{},ErrQuoteStateConflict }
	var revisionNo int
	if err=tx.QueryRow(ctx,`SELECT COALESCE(MAX(revision_no),0)+1 FROM order_price_revisions WHERE order_id=$1`,orderID).Scan(&revisionNo); err!=nil {
		return QuoteDecision{},err
	}
	revisionID,err:=newID()
	if err!=nil { return QuoteDecision{},err }
	if _,err=tx.Exec(ctx,`
		INSERT INTO order_price_revisions(id,order_id,revision_no,source_type,source_id,previous_amount_minor,new_amount_minor,approved_by_identity_id)
		VALUES($1,$2,$3,'quote',$4,$5,$6,$7)
	`,revisionID,orderID,revisionNo,quoteID,currentAmount,quoteTotal,customerID); err!=nil {
		return QuoteDecision{},err
	}
	if currentAmount!=quoteTotal {
		if _,err=tx.Exec(ctx,`UPDATE orders SET amount_minor=$2,updated_at=now() WHERE id=$1`,orderID,quoteTotal); err!=nil {
			return QuoteDecision{},err
		}
	}
	decision,err:=s.appendQuoteDecision(ctx,tx,quoteID,actor,"accepted","")
	if err!=nil { return QuoteDecision{},err }
	if err=tx.Commit(ctx); err!=nil { return QuoteDecision{},err }
	return decision,nil
}
