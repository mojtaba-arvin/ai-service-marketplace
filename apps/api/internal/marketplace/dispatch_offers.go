package marketplace

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

var (
	ErrInvalidDispatchFanout = errors.New("dispatch fanout must be between 1 and 10")
	ErrDispatchRoundActive = errors.New("order already has an active dispatch round")
	ErrDispatchOfferUnavailable = errors.New("dispatch offer is no longer available")
	ErrDispatchOfferExpired = errors.New("dispatch offer has expired")
	ErrOrderAlreadyAssigned = errors.New("another provider already accepted this order")
)

type DispatchOffer struct {
	ID string `json:"id"`
	RoundID string `json:"round_id"`
	OrderID string `json:"order_id"`
	ProviderID string `json:"provider_id"`
	StartAt time.Time `json:"start_at"`
	EndAt time.Time `json:"end_at"`
	Timezone string `json:"timezone"`
	Status string `json:"status"`
	ExpiresAt time.Time `json:"expires_at"`
	CreatedAt time.Time `json:"created_at"`
}

type DispatchAssignment struct {
	ID string `json:"id"`
	OrderID string `json:"order_id"`
	ProviderID string `json:"provider_id"`
	OfferID string `json:"offer_id"`
	CreatedAt time.Time `json:"created_at"`
}

func dispatchProviderEligibleTx(ctx context.Context,tx pgx.Tx,tenantID,providerID,categoryID,country,region,city,area string,startAt,endAt time.Time) (bool,error) {
	var eligible bool
	err:=tx.QueryRow(ctx,`
		WITH RECURSIVE ancestors AS (
			SELECT id,parent_id,enabled FROM service_categories WHERE id=$3
			UNION ALL
			SELECT c.id,c.parent_id,c.enabled FROM service_categories c JOIN ancestors a ON c.id=a.parent_id
		)
		SELECT EXISTS(
			SELECT 1 FROM provider_profiles p
			JOIN provider_skills ps ON ps.provider_id=p.id AND ps.category_id=$3 AND ps.verified_at IS NOT NULL
			JOIN provider_service_areas sa ON sa.provider_id=p.id AND sa.active=true
			JOIN markets m ON m.code=sa.country_code AND m.enabled=true
			WHERE p.id=$2 AND p.tenant_id=$1 AND p.active=true AND p.verified_at IS NOT NULL
			  AND sa.country_code=$4 AND sa.region_code=$5 AND sa.city_code=$6 AND sa.area_code=$7
			  AND (SELECT COALESCE(bool_and(enabled),false) FROM ancestors)
			  AND EXISTS(SELECT 1 FROM provider_availability_windows w
				WHERE w.provider_id=p.id AND w.window_type='available' AND w.start_at<=$8 AND w.end_at>=$9)
			  AND NOT EXISTS(SELECT 1 FROM provider_availability_windows w
				WHERE w.provider_id=p.id AND w.window_type='unavailable'
				  AND tstzrange(w.start_at,w.end_at,'[)') && tstzrange($8,$9,'[)'))
			  AND NOT EXISTS(SELECT 1 FROM provider_bookings b
				WHERE b.provider_id=p.id AND b.status='booked'
				  AND tstzrange(b.start_at,b.end_at,'[)') && tstzrange($8,$9,'[)'))
		)
	`,tenantID,providerID,categoryID,country,region,city,area,startAt,endAt).Scan(&eligible)
	return eligible,err
}

func (s *Store) CreateDispatchOffers(ctx context.Context,actor Actor,orderID string,providerIDs []string,maxFanout int,expiresAt time.Time) ([]DispatchOffer,error) {
	if actor.Role!=RoleOperator || actor.SubjectID=="" || actor.TenantID=="" { return nil,ErrForbidden }
	if err:=Authorize(actor,Resource{TenantID:actor.TenantID},ActionOrderAssign); err!=nil { return nil,err }
	if maxFanout<1 || maxFanout>10 || len(providerIDs)<1 || len(providerIDs)>maxFanout || len(providerIDs)>10 {
		return nil,ErrInvalidDispatchFanout
	}
	seen:=make(map[string]bool,len(providerIDs))
	for _,id:=range providerIDs {
		if strings.TrimSpace(id)=="" || seen[id] { return nil,ErrInvalidDispatchFanout }
		seen[id]=true
	}
	now:=time.Now().UTC()
	if expiresAt.Before(now.Add(time.Minute)) || expiresAt.After(now.Add(30*time.Minute)) { return nil,ErrDispatchOfferExpired }
	candidates,err:=s.FindDispatchCandidatesForOrder(ctx,actor,orderID,time.Time{},time.Time{})
	if err!=nil { return nil,err }
	eligibleSet:=make(map[string]bool,len(candidates))
	for _,candidate:=range candidates { eligibleSet[candidate.ProviderID]=true }
	for _,id:=range providerIDs { if !eligibleSet[id] { return nil,ErrProviderUnavailable } }

	tx,err:=s.DB.BeginTx(ctx,pgx.TxOptions{})
	if err!=nil { return nil,err }
	defer tx.Rollback(ctx)
	var status,country,region,city,area string
	var scheduledAt *time.Time
	var priceVersionID *string
	var assignedProviderID *string
	err=tx.QueryRow(ctx,`
		SELECT status,scheduled_at,price_version_id,country_code,region_code,city_code,area_code,assigned_provider_id
		FROM orders WHERE id=$1 FOR UPDATE
	`,orderID).Scan(&status,&scheduledAt,&priceVersionID,&country,&region,&city,&area,&assignedProviderID)
	if errors.Is(err,pgx.ErrNoRows) { return nil,ErrOrderNotFound }
	if err!=nil { return nil,err }
	if status!="requested" || scheduledAt==nil || priceVersionID==nil || assignedProviderID!=nil { return nil,ErrOrderAlreadyAssigned }
	var activeRound bool
	err=tx.QueryRow(ctx,`
		SELECT EXISTS(SELECT 1 FROM dispatch_offers WHERE order_id=$1 AND status='offered' AND expires_at>now())
	`,orderID).Scan(&activeRound)
	if err!=nil { return nil,err }
	if activeRound { return nil,ErrDispatchRoundActive }
	var categoryID,timezone string
	err=tx.QueryRow(ctx,`
		SELECT m.category_id,market.timezone
		FROM service_price_versions v JOIN service_price_menus m ON m.id=v.menu_id
		JOIN markets market ON market.code=$2 AND market.enabled=true
		WHERE v.id=$1
	`,*priceVersionID,country).Scan(&categoryID,&timezone)
	if errors.Is(err,pgx.ErrNoRows) { return nil,ErrProviderUnavailable }
	if err!=nil { return nil,err }
	startAt:=scheduledAt.Add(-30*time.Minute).UTC().Truncate(time.Microsecond)
	endAt:=scheduledAt.Add(30*time.Minute).UTC().Truncate(time.Microsecond)
	for _,providerID:=range providerIDs {
		ok,checkErr:=dispatchProviderEligibleTx(ctx,tx,actor.TenantID,providerID,categoryID,country,region,city,area,startAt,endAt)
		if checkErr!=nil { return nil,checkErr }
		if !ok { return nil,ErrProviderUnavailable }
	}
	var roundNo int
	if err=tx.QueryRow(ctx,`SELECT COALESCE(MAX(round_no),0)+1 FROM dispatch_rounds WHERE order_id=$1`,orderID).Scan(&roundNo); err!=nil {
		return nil,err
	}
	roundID,err:=newID()
	if err!=nil { return nil,err }
	_,err=tx.Exec(ctx,`
		INSERT INTO dispatch_rounds(id,order_id,round_no,fanout_limit,offer_count,expires_at,created_by_identity_id)
		VALUES($1,$2,$3,$4,$5,$6,$7)
	`,roundID,orderID,roundNo,maxFanout,len(providerIDs),expiresAt,actor.SubjectID)
	if err!=nil { return nil,err }
	offers:=make([]DispatchOffer,0,len(providerIDs))
	for _,providerID:=range providerIDs {
		id,genErr:=newID()
		if genErr!=nil { return nil,genErr }
		var offer DispatchOffer
		err=tx.QueryRow(ctx,`
			INSERT INTO dispatch_offers(id,round_id,order_id,provider_id,start_at,end_at,timezone,expires_at)
			VALUES($1,$2,$3,$4,$5,$6,$7,$8)
			RETURNING id,round_id,order_id,provider_id,start_at,end_at,timezone,status,expires_at,created_at
		`,id,roundID,orderID,providerID,startAt,endAt,timezone,expiresAt).Scan(&offer.ID,&offer.RoundID,&offer.OrderID,
			&offer.ProviderID,&offer.StartAt,&offer.EndAt,&offer.Timezone,&offer.Status,&offer.ExpiresAt,&offer.CreatedAt)
		if err!=nil { return nil,err }
		payload,_:=json.Marshal(map[string]any{"offer_id":offer.ID,"order_id":orderID,"provider_id":providerID,"expires_at":expiresAt})
		if _,err=tx.Exec(ctx,`INSERT INTO outbox_events(aggregate_type,aggregate_id,event_type,payload) VALUES('dispatch_offer',$1,'dispatch.offer_created',$2::jsonb)`,offer.ID,payload); err!=nil {
			return nil,err
		}
		offers=append(offers,offer)
	}
	if err=tx.Commit(ctx); err!=nil { return nil,err }
	return offers,nil
}

func (s *Store) AcceptDispatchOffer(ctx context.Context,actor Actor,offerID string) (DispatchAssignment,error) {
	if actor.Role!=RoleProfessional || actor.SubjectID=="" || actor.TenantID=="" { return DispatchAssignment{},ErrForbidden }
	tx,err:=s.DB.BeginTx(ctx,pgx.TxOptions{})
	if err!=nil { return DispatchAssignment{},err }
	defer tx.Rollback(ctx)
	var orderID,providerID,providerIdentity,providerTenant,offerStatus,timezone string
	var expiresAt,startAt,endAt,scheduledAt time.Time
	var orderStatus,country,region,city,area string
	var priceVersionID *string
	var assignedProviderID *string
	err=tx.QueryRow(ctx,`
		SELECT off.order_id,off.provider_id,p.identity_id,p.tenant_id,off.status,off.expires_at,
			off.start_at,off.end_at,off.timezone,o.status,o.scheduled_at,o.price_version_id,
			o.country_code,o.region_code,o.city_code,o.area_code,o.assigned_provider_id
		FROM dispatch_offers off JOIN provider_profiles p ON p.id=off.provider_id
		JOIN orders o ON o.id=off.order_id
		WHERE off.id=$1 FOR UPDATE OF off,o,p
	`,offerID).Scan(&orderID,&providerID,&providerIdentity,&providerTenant,&offerStatus,&expiresAt,
		&startAt,&endAt,&timezone,&orderStatus,&scheduledAt,&priceVersionID,&country,&region,&city,&area,&assignedProviderID)
	if errors.Is(err,pgx.ErrNoRows) { return DispatchAssignment{},ErrDispatchOfferUnavailable }
	if err!=nil { return DispatchAssignment{},err }
	if providerIdentity!=actor.SubjectID { return DispatchAssignment{},ErrForbidden }
	if err=Authorize(actor,Resource{TenantID:providerTenant,OwnerID:providerIdentity},ActionDispatchOfferRespond); err!=nil {
		return DispatchAssignment{},err
	}
	if offerStatus!="offered" { return DispatchAssignment{},ErrDispatchOfferUnavailable }
	if !time.Now().Before(expiresAt) { return DispatchAssignment{},ErrDispatchOfferExpired }
	if orderStatus!="requested" || assignedProviderID!=nil { return DispatchAssignment{},ErrOrderAlreadyAssigned }
	if priceVersionID==nil || scheduledAt.Before(startAt) || !scheduledAt.Before(endAt) { return DispatchAssignment{},ErrProviderUnavailable }
	var categoryID string
	err=tx.QueryRow(ctx,`
		SELECT m.category_id FROM service_price_versions v JOIN service_price_menus m ON m.id=v.menu_id WHERE v.id=$1
	`,*priceVersionID).Scan(&categoryID)
	if err!=nil { return DispatchAssignment{},err }
	eligible,err:=dispatchProviderEligibleTx(ctx,tx,providerTenant,providerID,categoryID,country,region,city,area,startAt,endAt)
	if err!=nil { return DispatchAssignment{},err }
	if !eligible { return DispatchAssignment{},ErrProviderUnavailable }

	tag,err:=tx.Exec(ctx,`UPDATE dispatch_offers SET status='accepted',decided_at=now() WHERE id=$1 AND status='offered'`,offerID)
	if err!=nil { return DispatchAssignment{},err }
	if tag.RowsAffected()!=1 { return DispatchAssignment{},ErrDispatchOfferUnavailable }
	assignmentID,err:=newID()
	if err!=nil { return DispatchAssignment{},err }
	var assignment DispatchAssignment
	err=tx.QueryRow(ctx,`
		INSERT INTO dispatch_assignments(id,order_id,provider_id,offer_id)
		VALUES($1,$2,$3,$4)
		RETURNING id,order_id,provider_id,offer_id,created_at
	`,assignmentID,orderID,providerID,offerID).Scan(&assignment.ID,&assignment.OrderID,&assignment.ProviderID,&assignment.OfferID,&assignment.CreatedAt)
	if err!=nil {
		var pgErr *pgconn.PgError
		if errors.As(err,&pgErr) && pgErr.Code=="23505" { return DispatchAssignment{},ErrOrderAlreadyAssigned }
		return DispatchAssignment{},err
	}
	bookingID,err:=newID()
	if err!=nil { return DispatchAssignment{},err }
	_,err=tx.Exec(ctx,`
		INSERT INTO provider_bookings(id,provider_id,order_id,start_at,end_at,timezone,created_by_identity_id)
		VALUES($1,$2,$3,$4,$5,$6,$7)
	`,bookingID,providerID,orderID,startAt,endAt,timezone,actor.SubjectID)
	if err!=nil {
		var pgErr *pgconn.PgError
		if errors.As(err,&pgErr) && pgErr.Code=="23P01" { return DispatchAssignment{},ErrProviderBookingConflict }
		if errors.As(err,&pgErr) && pgErr.Code=="23505" { return DispatchAssignment{},ErrOrderAlreadyAssigned }
		return DispatchAssignment{},err
	}
	if _,err=tx.Exec(ctx,`
		UPDATE orders SET status='accepted',assigned_provider_id=$2,updated_at=now()
		WHERE id=$1 AND status='requested' AND assigned_provider_id IS NULL
	`,orderID,providerID); err!=nil { return DispatchAssignment{},err }
	if _,err=tx.Exec(ctx,`
		UPDATE dispatch_offers SET status='superseded',decided_at=now()
		WHERE order_id=$1 AND id<>$2 AND status='offered'
	`,orderID,offerID); err!=nil { return DispatchAssignment{},err }
	payload,_:=json.Marshal(map[string]any{"order_id":orderID,"provider_id":providerID,"assignment_id":assignment.ID,"offer_id":offerID})
	if _,err=tx.Exec(ctx,`INSERT INTO outbox_events(aggregate_type,aggregate_id,event_type,payload) VALUES('order',$1,'order.provider_assigned',$2::jsonb)`,orderID,payload); err!=nil {
		return DispatchAssignment{},err
	}
	if err=tx.Commit(ctx); err!=nil { return DispatchAssignment{},err }
	return assignment,nil
}

func (s *Store) DeclineDispatchOffer(ctx context.Context,actor Actor,offerID string) error {
	if actor.Role!=RoleProfessional || actor.SubjectID=="" || actor.TenantID=="" { return ErrForbidden }
	tx,err:=s.DB.BeginTx(ctx,pgx.TxOptions{})
	if err!=nil { return err }
	defer tx.Rollback(ctx)
	var identityID,tenantID,status string
	var expiresAt time.Time
	var orderID string
	err=tx.QueryRow(ctx,`
		SELECT p.identity_id,p.tenant_id,off.status,off.expires_at,off.order_id
		FROM dispatch_offers off JOIN provider_profiles p ON p.id=off.provider_id
		WHERE off.id=$1 FOR UPDATE OF off
	`,offerID).Scan(&identityID,&tenantID,&status,&expiresAt,&orderID)
	if errors.Is(err,pgx.ErrNoRows) { return ErrDispatchOfferUnavailable }
	if err!=nil { return err }
	if identityID!=actor.SubjectID { return ErrForbidden }
	if err=Authorize(actor,Resource{TenantID:tenantID,OwnerID:identityID},ActionDispatchOfferRespond); err!=nil { return err }
	if status!="offered" { return ErrDispatchOfferUnavailable }
	if !time.Now().Before(expiresAt) { return ErrDispatchOfferExpired }
	if _,err=tx.Exec(ctx,`UPDATE dispatch_offers SET status='declined',decided_at=now() WHERE id=$1 AND status='offered'`,offerID); err!=nil { return err }
	payload,_:=json.Marshal(map[string]any{"offer_id":offerID,"order_id":orderID,"provider_id":actor.SubjectID})
	if _,err=tx.Exec(ctx,`INSERT INTO outbox_events(aggregate_type,aggregate_id,event_type,payload) VALUES('dispatch_offer',$1,'dispatch.offer_declined',$2::jsonb)`,offerID,payload); err!=nil { return err }
	return tx.Commit(ctx)
}

func (s *Store) ExpireDispatchOffers(ctx context.Context,limit int) (int,error) {
	if limit<1 { limit=100 }
	if limit>500 { limit=500 }
	tx,err:=s.DB.BeginTx(ctx,pgx.TxOptions{})
	if err!=nil { return 0,err }
	defer tx.Rollback(ctx)
	rows,err:=tx.Query(ctx,`
		WITH expired AS (
			SELECT id,order_id FROM dispatch_offers
			WHERE status='offered' AND expires_at<=now()
			ORDER BY expires_at,id FOR UPDATE SKIP LOCKED LIMIT $1
		)
		UPDATE dispatch_offers off SET status='expired',decided_at=now()
		FROM expired e WHERE off.id=e.id
		RETURNING off.id,off.order_id
	`,limit)
	if err!=nil { return 0,err }
	type expiredOffer struct { id,orderID string }
	expired:=make([]expiredOffer,0)
	for rows.Next() {
		var item expiredOffer
		if err=rows.Scan(&item.id,&item.orderID); err!=nil { rows.Close(); return 0,err }
		expired=append(expired,item)
	}
	if err=rows.Err(); err!=nil { rows.Close(); return 0,err }
	rows.Close()
	for _,item:=range expired {
		payload,_:=json.Marshal(map[string]any{"offer_id":item.id,"order_id":item.orderID})
		if _,err=tx.Exec(ctx,`INSERT INTO outbox_events(aggregate_type,aggregate_id,event_type,payload) VALUES('dispatch_offer',$1,'dispatch.offer_expired',$2::jsonb)`,item.id,payload); err!=nil {
			return 0,err
		}
	}
	if err=tx.Commit(ctx); err!=nil { return 0,err }
	return len(expired),nil
}

var _ = fmt.Sprintf
