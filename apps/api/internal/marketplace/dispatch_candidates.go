package marketplace

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

type DispatchCandidate struct {
	ProviderID string `json:"provider_id"`
	IdentityID string `json:"identity_id"`
	DisplayName string `json:"display_name"`
}

func (s *Store) FindDispatchCandidates(ctx context.Context,actor Actor,categoryID,country,region,city,area string,
	startAt,endAt time.Time) ([]DispatchCandidate,error) {
	country=strings.ToUpper(strings.TrimSpace(country))
	region=strings.TrimSpace(region)
	city=strings.TrimSpace(city)
	area=strings.TrimSpace(area)
	if actor.SubjectID=="" || actor.TenantID=="" { return nil,ErrForbidden }
	switch actor.Role {
	case RoleCustomer:
		if err:=Authorize(actor,Resource{TenantID:actor.TenantID,OwnerID:actor.SubjectID},ActionOrderCreate); err!=nil { return nil,err }
	case RoleOperator:
		// The persisted grant is checked under the read transaction below.
	default:
		return nil,ErrForbidden
	}
	if categoryID=="" || len(country)!=2 || region=="" || city=="" || area=="" ||
		!endAt.After(startAt) || endAt.Sub(startAt)>24*time.Hour {
		return nil,ErrInvalidAvailability
	}
	startAt=startAt.UTC()
	endAt=endAt.UTC()
	tx,err:=s.DB.BeginTx(ctx,pgx.TxOptions{})
	if err!=nil { return nil,err }
	defer tx.Rollback(ctx)
	if actor.Role==RoleOperator {
		if err=authorizeOperatorTx(ctx,tx,actor,Resource{TenantID:actor.TenantID},ActionOrderAssign);err!=nil{return nil,err}
	}
	rows,err:=tx.Query(ctx,`
		WITH RECURSIVE ancestors AS (
			SELECT id,parent_id,enabled FROM service_categories WHERE id=$1
			UNION ALL
			SELECT c.id,c.parent_id,c.enabled FROM service_categories c JOIN ancestors a ON c.id=a.parent_id
		)
		SELECT p.id,p.identity_id,p.display_name
		FROM provider_profiles p
		JOIN provider_skills ps ON ps.provider_id=p.id AND ps.category_id=$1 AND ps.verified_at IS NOT NULL
		JOIN provider_service_areas sa ON sa.provider_id=p.id AND sa.active=true
		JOIN markets m ON m.code=sa.country_code AND m.enabled=true
		WHERE p.active=true AND p.verified_at IS NOT NULL AND p.tenant_id=$8
		  AND sa.country_code=$2 AND sa.region_code=$3 AND sa.city_code=$4 AND sa.area_code=$5
		  AND (SELECT COALESCE(bool_and(enabled),false) FROM ancestors)
		  AND EXISTS (
			SELECT 1 FROM provider_availability_windows w
			WHERE w.provider_id=p.id AND w.window_type='available'
			  AND w.start_at<=$6 AND w.end_at>=$7
		  )
		  AND NOT EXISTS (
			SELECT 1 FROM provider_availability_windows w
			WHERE w.provider_id=p.id AND w.window_type='unavailable'
			  AND tstzrange(w.start_at,w.end_at,'[)') && tstzrange($6,$7,'[)')
		  )
		  AND NOT EXISTS (
			SELECT 1 FROM provider_bookings b
			WHERE b.provider_id=p.id AND b.status='booked'
			  AND tstzrange(b.start_at,b.end_at,'[)') && tstzrange($6,$7,'[)')
		  )
		ORDER BY p.id
	`,categoryID,country,region,city,area,startAt,endAt,actor.TenantID)
	if err!=nil { return nil,err }
	defer rows.Close()
	candidates:=make([]DispatchCandidate,0)
	for rows.Next() {
		var candidate DispatchCandidate
		if err=rows.Scan(&candidate.ProviderID,&candidate.IdentityID,&candidate.DisplayName); err!=nil { return nil,err }
		candidates=append(candidates,candidate)
	}
	if err=rows.Err(); err!=nil { return nil,err }
	if err=tx.Commit(ctx); err!=nil { return nil,err }
	return candidates,nil
}

func (s *Store) FindDispatchCandidatesForOrder(ctx context.Context,actor Actor,orderID string,startAt,endAt time.Time) ([]DispatchCandidate,error) {
	if actor.Role==RoleCustomer {
		if err:=Authorize(actor,Resource{TenantID:actor.TenantID,OwnerID:actor.SubjectID},ActionOrderRead); err!=nil { return nil,err }
	} else if actor.Role==RoleOperator {
		checkTx,beginErr:=s.DB.BeginTx(ctx,pgx.TxOptions{})
		if beginErr!=nil{return nil,beginErr}
		if beginErr=authorizeOperatorTx(ctx,checkTx,actor,Resource{TenantID:actor.TenantID},ActionOrderAssign);beginErr!=nil{
			_ = checkTx.Rollback(ctx)
			return nil,beginErr
		}
		if beginErr=checkTx.Commit(ctx);beginErr!=nil{return nil,beginErr}
	} else {
		return nil,ErrForbidden
	}
	var customerID,country,region,city,area,status string
	var priceVersionID *string
	var scheduledAt *time.Time
	err:=s.DB.QueryRow(ctx,`
		SELECT customer_id,price_version_id,country_code,region_code,city_code,area_code,scheduled_at,status
		FROM orders WHERE id=$1
	`,orderID).Scan(&customerID,&priceVersionID,&country,&region,&city,&area,&scheduledAt,&status)
	if err!=nil {
		if errors.Is(err,pgx.ErrNoRows) { return nil,ErrOrderNotFound }
		return nil,err
	}
	if actor.Role==RoleCustomer && customerID!=actor.SubjectID { return nil,ErrForbidden }
	if status!="requested" || priceVersionID==nil || scheduledAt==nil || country=="" || region=="" || city=="" || area=="" {
		return nil,ErrProviderUnavailable
	}
	var categoryID string
	err=s.DB.QueryRow(ctx,`
		SELECT m.category_id FROM service_price_versions v JOIN service_price_menus m ON m.id=v.menu_id WHERE v.id=$1
	`,*priceVersionID).Scan(&categoryID)
	if err!=nil { return nil,err }
	if startAt.IsZero() { startAt=scheduledAt.Add(-30*time.Minute) }
	if endAt.IsZero() { endAt=scheduledAt.Add(30*time.Minute) }
	return s.FindDispatchCandidates(ctx,actor,categoryID,country,region,city,area,startAt,endAt)
}
