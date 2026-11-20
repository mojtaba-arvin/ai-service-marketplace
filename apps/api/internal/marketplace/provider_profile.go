package marketplace

import (
    "context"
    "errors"

    "github.com/jackc/pgx/v5"
)

func (s *Store) GetProviderProfileForActor(ctx context.Context, actor Actor) (ProviderProfile, error) {
    if actor.Role != RoleProfessional || actor.SubjectID == "" || actor.TenantID == "" {
        return ProviderProfile{}, ErrForbidden
    }
    if err := Authorize(actor,Resource{TenantID:actor.TenantID,OwnerID:actor.SubjectID},ActionProfileUpdate); err != nil {
        return ProviderProfile{}, err
    }
    var profile ProviderProfile
    err := s.DB.QueryRow(ctx, `
        SELECT id,identity_id,tenant_id,display_name,active,verified_at,created_at
        FROM provider_profiles WHERE identity_id=$1 AND tenant_id=$2
    `,actor.SubjectID,actor.TenantID).Scan(&profile.ID,&profile.IdentityID,&profile.TenantID,
        &profile.DisplayName,&profile.Active,&profile.VerifiedAt,&profile.CreatedAt)
    if errors.Is(err,pgx.ErrNoRows) {
        return ProviderProfile{}, ErrProviderNotFound
    }
    return profile,err
}
