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
	ErrProviderNotFound = errors.New("provider profile not found")
	ErrProviderNotEligible = errors.New("provider requires verified profile, verified skill, and active service area")
)

type ProviderProfile struct {
	ID          string     `json:"id"`
	IdentityID  string     `json:"identity_id"`
	TenantID    string     `json:"tenant_id"`
	DisplayName string     `json:"display_name"`
	Active      bool       `json:"active"`
	VerifiedAt  *time.Time `json:"verified_at,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
}

type ProviderCandidate struct {
	ProviderID string `json:"provider_id"`
	IdentityID string `json:"identity_id"`
	DisplayName string `json:"display_name"`
}

func (s *Store) CreateProviderProfile(ctx context.Context, actor Actor, displayName string) (ProviderProfile, error) {
	displayName = strings.TrimSpace(displayName)
	if actor.Role != RoleProfessional || actor.SubjectID == "" || actor.TenantID == "" || displayName == "" || len(displayName) > 160 {
		return ProviderProfile{}, ErrForbidden
	}
	if err := Authorize(actor, Resource{TenantID: actor.TenantID, OwnerID: actor.SubjectID}, ActionProfileUpdate); err != nil {
		return ProviderProfile{}, err
	}
	id, err := newID()
	if err != nil { return ProviderProfile{}, err }
	var profile ProviderProfile
	err = s.DB.QueryRow(ctx, `
		INSERT INTO provider_profiles(id,identity_id,tenant_id,display_name)
		VALUES($1,$2,$3,$4)
		RETURNING id,identity_id,tenant_id,display_name,active,verified_at,created_at
	`, id, actor.SubjectID, actor.TenantID, displayName).Scan(&profile.ID,&profile.IdentityID,&profile.TenantID,
		&profile.DisplayName,&profile.Active,&profile.VerifiedAt,&profile.CreatedAt)
	if err != nil { return ProviderProfile{}, fmt.Errorf("create provider profile: %w", err) }
	return profile,nil
}

func (s *Store) authorizeProvider(ctx context.Context, actor Actor, providerID string, action Action) (string,error) {
	var identityID,tenantID string
	err := s.DB.QueryRow(ctx,`SELECT identity_id,tenant_id FROM provider_profiles WHERE id=$1`,providerID).Scan(&identityID,&tenantID)
	if errors.Is(err,pgx.ErrNoRows) { return "",ErrProviderNotFound }
	if err != nil { return "",err }
	if err = Authorize(actor,Resource{TenantID:tenantID,OwnerID:identityID},action); err != nil { return "",err }
	return identityID,nil
}

func (s *Store) AddProviderSkill(ctx context.Context, actor Actor, providerID, categoryID string) error {
	if _,err:=s.authorizeProvider(ctx,actor,providerID,ActionProfileUpdate); err!=nil { return err }
	enabled,err:=s.CanAcceptOrders(ctx,categoryID)
	if err!=nil { return err }
	if !enabled { return ErrCategoryDisabled }
	id,err:=newID()
	if err!=nil { return err }
	_,err=s.DB.Exec(ctx,`INSERT INTO provider_skills(id,provider_id,category_id) VALUES($1,$2,$3)`,id,providerID,categoryID)
	return err
}

func (s *Store) AddProviderServiceArea(ctx context.Context, actor Actor, providerID,country,region,city,area string) error {
	if _,err:=s.authorizeProvider(ctx,actor,providerID,ActionProfileUpdate); err!=nil { return err }
	country=strings.ToUpper(strings.TrimSpace(country))
	region=strings.TrimSpace(region); city=strings.TrimSpace(city); area=strings.TrimSpace(area)
	if len(country)!=2 || country[0]<'A' || country[0]>'Z' || country[1]<'A' || country[1]>'Z' ||
		region=="" || city=="" || area=="" || len(region)>100 || len(city)>100 || len(area)>100 {
		return errors.New("invalid provider service area")
	}
	id,err:=newID()
	if err!=nil { return err }
	_,err=s.DB.Exec(ctx,`
		INSERT INTO provider_service_areas(id,provider_id,country_code,region_code,city_code,area_code)
		VALUES($1,$2,$3,$4,$5,$6)
	`,id,providerID,country,region,city,area)
	return err
}

func (s *Store) VerifyProviderProfile(ctx context.Context, actor Actor, providerID string) error {
	if _,err:=s.authorizeProvider(ctx,actor,providerID,ActionProviderVerify); err!=nil { return err }
	tag,err:=s.DB.Exec(ctx,`
		UPDATE provider_profiles SET verified_at=COALESCE(verified_at,now()),
			verified_by_identity_id=COALESCE(verified_by_identity_id,$2),updated_at=now()
		WHERE id=$1
	`,providerID,actor.SubjectID)
	if err!=nil { return err }
	if tag.RowsAffected()!=1 { return ErrProviderNotFound }
	return nil
}

func (s *Store) VerifyProviderSkill(ctx context.Context, actor Actor, providerID, categoryID string) error {
	if _,err:=s.authorizeProvider(ctx,actor,providerID,ActionProviderVerify); err!=nil { return err }
	tag,err:=s.DB.Exec(ctx,`
		UPDATE provider_skills SET verified_at=COALESCE(verified_at,now()),
			verified_by_identity_id=COALESCE(verified_by_identity_id,$3)
		WHERE provider_id=$1 AND category_id=$2
	`,providerID,categoryID,actor.SubjectID)
	if err!=nil { return err }
	if tag.RowsAffected()!=1 { return ErrProviderNotEligible }
	return nil
}

func (s *Store) SetProviderActive(ctx context.Context, actor Actor, providerID string, active bool) error {
	if _,err:=s.authorizeProvider(ctx,actor,providerID,ActionProviderVerify); err!=nil { return err }
	if active {
		var eligible bool
		err:=s.DB.QueryRow(ctx,`
			SELECT p.verified_at IS NOT NULL
				AND EXISTS(SELECT 1 FROM provider_skills ps WHERE ps.provider_id=p.id AND ps.verified_at IS NOT NULL)
				AND EXISTS(SELECT 1 FROM provider_service_areas sa WHERE sa.provider_id=p.id AND sa.active=true)
			FROM provider_profiles p WHERE p.id=$1
		`,providerID).Scan(&eligible)
		if errors.Is(err,pgx.ErrNoRows) { return ErrProviderNotFound }
		if err!=nil { return err }
		if !eligible { return ErrProviderNotEligible }
	}
	tag,err:=s.DB.Exec(ctx,`UPDATE provider_profiles SET active=$2,updated_at=now() WHERE id=$1`,providerID,active)
	if err!=nil { return err }
	if tag.RowsAffected()!=1 { return ErrProviderNotFound }
	return nil
}

// FindEligibleProviders fails closed unless the category and every ancestor are
// enabled, the profile is active/verified, the skill is verified, and the
// service-area tuple matches exactly.
func (s *Store) FindEligibleProviders(ctx context.Context, categoryID,country,region,city,area string) ([]ProviderCandidate,error) {
	country=strings.ToUpper(strings.TrimSpace(country))
	if len(country)!=2 || region=="" || city=="" || area=="" { return nil,errors.New("complete service area is required") }
	rows,err:=s.DB.Query(ctx,`
		WITH RECURSIVE ancestors AS (
			SELECT id,parent_id,enabled FROM service_categories WHERE id=$1
			UNION ALL
			SELECT c.id,c.parent_id,c.enabled FROM service_categories c JOIN ancestors a ON c.id=a.parent_id
		)
		SELECT DISTINCT p.id,p.identity_id,p.display_name
		FROM provider_profiles p
		JOIN provider_skills ps ON ps.provider_id=p.id AND ps.category_id=$1 AND ps.verified_at IS NOT NULL
		JOIN provider_service_areas sa ON sa.provider_id=p.id AND sa.active=true
		WHERE p.active=true AND p.verified_at IS NOT NULL
		  AND sa.country_code=$2 AND sa.region_code=$3 AND sa.city_code=$4 AND sa.area_code=$5
		  AND (SELECT COALESCE(bool_and(enabled),false) FROM ancestors)
		ORDER BY p.id
	`,categoryID,country,region,city,area)
	if err!=nil { return nil,err }
	defer rows.Close()
	candidates:=make([]ProviderCandidate,0)
	for rows.Next() {
		var candidate ProviderCandidate
		if err=rows.Scan(&candidate.ProviderID,&candidate.IdentityID,&candidate.DisplayName); err!=nil { return nil,err }
		candidates=append(candidates,candidate)
	}
	if err=rows.Err(); err!=nil { return nil,err }
	return candidates,nil
}
