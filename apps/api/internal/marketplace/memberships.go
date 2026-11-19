package marketplace

import (
    "context"
    "errors"
    "strings"
)

var ErrMembershipNotFound = errors.New("identity has no membership for the requested tenant and role")

func (s *Store) EnsureIdentityMembership(ctx context.Context, identityID, tenantID string, role Role) error {
    tenantID = strings.TrimSpace(tenantID)
    if identityID == "" || tenantID == "" || len(tenantID) > 120 || !validRole(role) {
        return ErrForbidden
    }
    id, err := newID()
    if err != nil {
        return err
    }
    _, err = s.DB.Exec(ctx, `
        INSERT INTO identity_memberships(id,identity_id,tenant_id,role)
        VALUES($1,$2,$3,$4)
        ON CONFLICT(identity_id,tenant_id,role) DO NOTHING
    `, id, identityID, tenantID, string(role))
    return err
}

func (s *Store) EnsureMembershipForPhone(ctx context.Context, phone, tenantID string, role Role) error {
    normalized, err := NormalizeE164Phone(phone)
    if err != nil {
        return err
    }
    var identityID string
    if err = s.DB.QueryRow(ctx, `SELECT id FROM phone_identities WHERE phone_e164=$1`, normalized).Scan(&identityID); err != nil {
        return err
    }
    return s.EnsureIdentityMembership(ctx, identityID, tenantID, role)
}

func (s *Store) ResolveActor(ctx context.Context, identityID, tenantID string, role Role) (Actor, error) {
    if identityID == "" || strings.TrimSpace(tenantID) == "" || !validRole(role) {
        return Actor{}, ErrForbidden
    }
    var exists bool
    err := s.DB.QueryRow(ctx, `
        SELECT EXISTS(
            SELECT 1 FROM identity_memberships
            WHERE identity_id=$1 AND tenant_id=$2 AND role=$3
        )
    `, identityID, strings.TrimSpace(tenantID), string(role)).Scan(&exists)
    if err != nil {
        return Actor{}, err
    }
    if !exists {
        return Actor{}, ErrMembershipNotFound
    }
    return Actor{SubjectID: identityID, TenantID: strings.TrimSpace(tenantID), Role: role}, nil
}

func (s *Store) ListIdentityRoles(ctx context.Context, identityID, tenantID string) ([]Role, error) {
    if identityID == "" || strings.TrimSpace(tenantID) == "" {
        return nil, ErrForbidden
    }
    rows, err := s.DB.Query(ctx, `
        SELECT role FROM identity_memberships
        WHERE identity_id=$1 AND tenant_id=$2 ORDER BY role
    `, identityID, strings.TrimSpace(tenantID))
    if err != nil {
        return nil, err
    }
    defer rows.Close()
    roles := make([]Role, 0, 2)
    for rows.Next() {
        var role string
        if err = rows.Scan(&role); err != nil {
            return nil, err
        }
        roles = append(roles, Role(role))
    }
    if err = rows.Err(); err != nil {
        return nil, err
    }
    return roles, nil
}
