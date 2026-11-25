package marketplace

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/mojtaba-arvin/ai-service-marketplace/apps/api/internal/outbox"
)

var (
	ErrInvalidAvailability = errors.New("invalid provider availability window")
	ErrAvailabilityOverlap = errors.New("provider availability overlaps an existing booking or window")
	ErrProviderUnavailable = errors.New("provider is not available for the requested time and area")
	ErrProviderBookingConflict = errors.New("provider already has an overlapping booking")
	ErrOrderAlreadyBooked = errors.New("order already has a provider booking")
)

type ProviderAvailabilityWindow struct {
	ID string `json:"id"`
	ProviderID string `json:"provider_id"`
	Type string `json:"type"`
	StartAt time.Time `json:"start_at"`
	EndAt time.Time `json:"end_at"`
	Timezone string `json:"timezone"`
	CreatedAt time.Time `json:"created_at"`
}

type ProviderBooking struct {
	ID string `json:"id"`
	ProviderID string `json:"provider_id"`
	OrderID string `json:"order_id"`
	StartAt time.Time `json:"start_at"`
	EndAt time.Time `json:"end_at"`
	Timezone string `json:"timezone"`
	Status string `json:"status"`
	CreatedAt time.Time `json:"created_at"`
}

func validateCalendarRange(startAt,endAt time.Time,timezone string,maxDuration time.Duration) error {
	if _,err:=time.LoadLocation(timezone); err!=nil { return ErrInvalidAvailability }
	startAt=startAt.UTC()
	endAt=endAt.UTC()
	if !endAt.After(startAt) || endAt.Sub(startAt)>maxDuration { return ErrInvalidAvailability }
	return nil
}

func (s *Store) SetProviderAvailability(ctx context.Context,actor Actor,providerID,windowType string,
	startAt,endAt time.Time,timezone string) (ProviderAvailabilityWindow,error) {
	windowType=strings.ToLower(strings.TrimSpace(windowType))
	timezone=strings.TrimSpace(timezone)
	if (windowType!="available" && windowType!="unavailable") ||
		validateCalendarRange(startAt,endAt,timezone,31*24*time.Hour)!=nil {
		return ProviderAvailabilityWindow{},ErrInvalidAvailability
	}
	if actor.SubjectID=="" || actor.TenantID=="" { return ProviderAvailabilityWindow{},ErrForbidden }
	tx,err:=s.DB.BeginTx(ctx,pgx.TxOptions{})
	if err!=nil { return ProviderAvailabilityWindow{},err }
	defer tx.Rollback(ctx)
	var identityID,tenantID string
	err=tx.QueryRow(ctx,`SELECT identity_id,tenant_id FROM provider_profiles WHERE id=$1 FOR UPDATE`,providerID).Scan(&identityID,&tenantID)
	if errors.Is(err,pgx.ErrNoRows) { return ProviderAvailabilityWindow{},ErrProviderNotEligible }
	if err!=nil { return ProviderAvailabilityWindow{},err }
	if actor.Role==RoleProfessional {
		if identityID!=actor.SubjectID { return ProviderAvailabilityWindow{},ErrForbidden }
		if err=Authorize(actor,Resource{TenantID:tenantID,OwnerID:identityID},ActionProfileUpdate); err!=nil {
			return ProviderAvailabilityWindow{},err
		}
	} else if actor.Role==RoleOperator {
		if err=authorizeOperatorTx(ctx,tx,actor,Resource{TenantID:actor.TenantID},ActionOrderAssign); err!=nil {
			return ProviderAvailabilityWindow{},err
		}
	} else {
		return ProviderAvailabilityWindow{},ErrForbidden
	}
	startAt=startAt.UTC().Truncate(time.Microsecond)
	endAt=endAt.UTC().Truncate(time.Microsecond)
	if windowType=="unavailable" {
		var overlaps bool
		err=tx.QueryRow(ctx,`
			SELECT EXISTS(SELECT 1 FROM provider_bookings
				WHERE provider_id=$1 AND status='booked'
				  AND tstzrange(start_at,end_at,'[)') && tstzrange($2,$3,'[)'))
		`,providerID,startAt,endAt).Scan(&overlaps)
		if err!=nil { return ProviderAvailabilityWindow{},err }
		if overlaps { return ProviderAvailabilityWindow{},ErrAvailabilityOverlap }
	}
	id,err:=newID()
	if err!=nil { return ProviderAvailabilityWindow{},err }
	var window ProviderAvailabilityWindow
	err=tx.QueryRow(ctx,`
		INSERT INTO provider_availability_windows(id,provider_id,window_type,start_at,end_at,timezone,created_by_identity_id)
		VALUES($1,$2,$3,$4,$5,$6,$7)
		RETURNING id,provider_id,window_type,start_at,end_at,timezone,created_at
	`,id,providerID,windowType,startAt,endAt,timezone,actor.SubjectID).Scan(&window.ID,&window.ProviderID,
		&window.Type,&window.StartAt,&window.EndAt,&window.Timezone,&window.CreatedAt)
	if err!=nil {
		var pgErr *pgconn.PgError
		if errors.As(err,&pgErr) && pgErr.Code=="23P01" { return ProviderAvailabilityWindow{},ErrAvailabilityOverlap }
		return ProviderAvailabilityWindow{},err
	}
	if err=tx.Commit(ctx); err!=nil { return ProviderAvailabilityWindow{},err }
	return window,nil
}

func (s *Store) CreateProviderBooking(ctx context.Context,actor Actor,providerID,orderID string,
	startAt,endAt time.Time,timezone string) (ProviderBooking,error) {
	if actor.Role!=RoleOperator || actor.SubjectID=="" || actor.TenantID=="" { return ProviderBooking{},ErrForbidden }
	if validateCalendarRange(startAt,endAt,timezone,24*time.Hour)!=nil { return ProviderBooking{},ErrInvalidAvailability }
	startAt=startAt.UTC().Truncate(time.Microsecond)
	endAt=endAt.UTC().Truncate(time.Microsecond)
	tx,err:=s.DB.BeginTx(ctx,pgx.TxOptions{})
	if err!=nil { return ProviderBooking{},err }
	defer tx.Rollback(ctx)
	if err=authorizeOperatorTx(ctx,tx,actor,Resource{TenantID:actor.TenantID},ActionOrderAssign);err!=nil{return ProviderBooking{},err}
	var active,verified bool
	var providerTenantID string
	err=tx.QueryRow(ctx,`SELECT active,verified_at IS NOT NULL,tenant_id FROM provider_profiles WHERE id=$1 FOR UPDATE`,providerID).Scan(&active,&verified,&providerTenantID)
	if errors.Is(err,pgx.ErrNoRows) { return ProviderBooking{},ErrProviderNotEligible }
	if err!=nil { return ProviderBooking{},err }
	if !active || !verified { return ProviderBooking{},ErrProviderNotEligible }
	if providerTenantID != actor.TenantID { return ProviderBooking{},ErrForbidden }

	var orderStatus,country,region,city,area,orderTenantID string
	var scheduledAt *time.Time
	var priceVersionID *string
	err=tx.QueryRow(ctx,`
		SELECT status,scheduled_at,price_version_id,country_code,region_code,city_code,area_code,tenant_id
		FROM orders WHERE id=$1 FOR UPDATE
	`,orderID).Scan(&orderStatus,&scheduledAt,&priceVersionID,&country,&region,&city,&area,&orderTenantID)
	if errors.Is(err,pgx.ErrNoRows) { return ProviderBooking{},ErrOrderNotFound }
	if err!=nil { return ProviderBooking{},err }
	if orderTenantID != actor.TenantID { return ProviderBooking{},ErrForbidden }
	if orderStatus!="requested" || scheduledAt==nil || priceVersionID==nil ||
		scheduledAt.Before(startAt) || !scheduledAt.Before(endAt) {
		return ProviderBooking{},ErrProviderUnavailable
	}
	var categoryID string
	if err=tx.QueryRow(ctx,`
		SELECT m.category_id FROM service_price_versions v JOIN service_price_menus m ON m.id=v.menu_id WHERE v.id=$1
	`,*priceVersionID).Scan(&categoryID); err!=nil { return ProviderBooking{},err }
	var eligible bool
	err=tx.QueryRow(ctx,`
		SELECT EXISTS(
			SELECT 1 FROM provider_skills s
			WHERE s.provider_id=$1 AND s.category_id=$2 AND s.verified_at IS NOT NULL
		) AND EXISTS(
			SELECT 1 FROM provider_service_areas a
			WHERE a.provider_id=$1 AND a.active=true AND a.country_code=$3
				AND a.region_code=$4 AND a.city_code=$5 AND a.area_code=$6
		)
	`,providerID,categoryID,country,region,city,area).Scan(&eligible)
	if err!=nil { return ProviderBooking{},err }
	if !eligible { return ProviderBooking{},ErrProviderNotEligible }

	var available bool
	err=tx.QueryRow(ctx,`
		SELECT EXISTS(
			SELECT 1 FROM provider_availability_windows w
			WHERE w.provider_id=$1 AND w.window_type='available' AND w.start_at<=$2 AND w.end_at>=$3
		) AND NOT EXISTS(
			SELECT 1 FROM provider_availability_windows w
			WHERE w.provider_id=$1 AND w.window_type='unavailable'
				AND tstzrange(w.start_at,w.end_at,'[)') && tstzrange($2,$3,'[)')
		)
	`,providerID,startAt,endAt).Scan(&available)
	if err!=nil { return ProviderBooking{},err }
	if !available { return ProviderBooking{},ErrProviderUnavailable }

	id,err:=newID()
	if err!=nil { return ProviderBooking{},err }
	var booking ProviderBooking
	err=tx.QueryRow(ctx,`
		INSERT INTO provider_bookings(id,provider_id,order_id,start_at,end_at,timezone,created_by_identity_id)
		VALUES($1,$2,$3,$4,$5,$6,$7)
		RETURNING id,provider_id,order_id,start_at,end_at,timezone,status,created_at
	`,id,providerID,orderID,startAt,endAt,timezone,actor.SubjectID).Scan(&booking.ID,&booking.ProviderID,
		&booking.OrderID,&booking.StartAt,&booking.EndAt,&booking.Timezone,&booking.Status,&booking.CreatedAt)
	if err!=nil {
		var pgErr *pgconn.PgError
		if errors.As(err,&pgErr) {
			if pgErr.Code=="23P01" { return ProviderBooking{},ErrProviderBookingConflict }
			if pgErr.Code=="23505" { return ProviderBooking{},ErrOrderAlreadyBooked }
		}
		return ProviderBooking{},err
	}
	eventID,err:=newID()
	if err!=nil { return ProviderBooking{},err }
	payload,err:=json.Marshal(map[string]any{"booking_id":booking.ID,"provider_id":providerID,"order_id":orderID,"start_at":startAt.UTC(),"end_at":endAt.UTC()})
	if err!=nil { return ProviderBooking{},err }
	if _,err=outbox.AppendDomainEvent(ctx,tx,outbox.DomainEvent{
		ID:eventID,Type:"provider.booking_reserved",Version:1,OccurredAt:time.Now().UTC(),
		TenantID:actor.TenantID,Aggregate:outbox.EventAggregate{Type:"provider_booking",ID:booking.ID},Data:payload,
	}); err!=nil {
		return ProviderBooking{},err
	}
	if err=tx.Commit(ctx); err!=nil { return ProviderBooking{},err }
	return booking,nil
}
