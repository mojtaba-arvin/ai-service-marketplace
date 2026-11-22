package marketplace

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"
)

var ErrOrganizationAccessDenied = errors.New("organization access denied")

type OrganizationSummary struct {
	ID string `json:"id"`
	TenantID string `json:"tenant_id"`
	Name string `json:"name"`
	Slug string `json:"slug"`
	Status string `json:"status"`
	Role string `json:"role"`
}

type PropertySummary struct {
	ID string `json:"id"`
	OrganizationID string `json:"organization_id"`
	TenantID string `json:"tenant_id"`
	Code string `json:"property_code"`
	Name string `json:"name"`
	CountryCode string `json:"country_code"`
	RegionCode string `json:"region_code"`
	CityCode string `json:"city_code"`
	AreaCode string `json:"area_code"`
	Status string `json:"status"`
	AccessRole string `json:"access_role"`
}

// ListOrganizationsForIdentity returns only organizations where the identity's
// organization membership is active. The check is performed on every request,
// rather than trusting a role or organization claim cached in an access token.
func (s *Store) ListOrganizationsForIdentity(ctx context.Context, identityID, tenantID string) ([]OrganizationSummary, error) {
	if strings.TrimSpace(identityID) == "" || strings.TrimSpace(tenantID) == "" {
		return nil, ErrOrganizationAccessDenied
	}
	rows, err := s.DB.Query(ctx, `
		SELECT o.id,o.tenant_id,o.name,o.slug,o.status,om.role
		FROM organizations o
		JOIN organization_memberships om
		  ON om.organization_id=o.id AND om.tenant_id=o.tenant_id
		JOIN identity_memberships im
		  ON im.identity_id=om.identity_id AND im.tenant_id=om.tenant_id
		 AND im.role=om.identity_role
		WHERE om.identity_id=$1 AND om.tenant_id=$2
		  AND om.status='active' AND o.status='active'
		ORDER BY o.name,o.id
	`, identityID, tenantID)
	if err != nil { return nil, err }
	defer rows.Close()
	items := make([]OrganizationSummary, 0)
	for rows.Next() {
		var item OrganizationSummary
		if err = rows.Scan(&item.ID,&item.TenantID,&item.Name,&item.Slug,&item.Status,&item.Role); err != nil { return nil, err }
		items = append(items,item)
	}
	if err = rows.Err(); err != nil { return nil, err }
	return items,nil
}

func (s *Store) GetOrganizationForIdentity(ctx context.Context, identityID, tenantID, organizationID string) (OrganizationSummary,error) {
	if strings.TrimSpace(identityID)=="" || strings.TrimSpace(tenantID)=="" || strings.TrimSpace(organizationID)=="" {
		return OrganizationSummary{},ErrOrganizationAccessDenied
	}
	var item OrganizationSummary
	err:=s.DB.QueryRow(ctx,`
		SELECT o.id,o.tenant_id,o.name,o.slug,o.status,om.role
		FROM organizations o
		JOIN organization_memberships om
		  ON om.organization_id=o.id AND om.tenant_id=o.tenant_id
		JOIN identity_memberships im
		  ON im.identity_id=om.identity_id AND im.tenant_id=om.tenant_id
		 AND im.role=om.identity_role
		WHERE o.id=$1 AND o.tenant_id=$2 AND om.identity_id=$3
		  AND om.status='active' AND o.status='active'
	`,organizationID,tenantID,identityID).Scan(&item.ID,&item.TenantID,&item.Name,&item.Slug,&item.Status,&item.Role)
	if errors.Is(err,pgx.ErrNoRows) { return OrganizationSummary{},ErrOrganizationAccessDenied }
	if err!=nil { return OrganizationSummary{},err }
	return item,nil
}

// ListPropertiesForIdentity gives organization administrators/managers the
// organization's active properties. Members/viewers see only properties
// explicitly delegated to them. Both paths require a currently active
// organization membership, so revocation takes effect on the next request.
func (s *Store) ListPropertiesForIdentity(ctx context.Context, identityID, tenantID, organizationID string) ([]PropertySummary,error) {
	if _,err:=s.GetOrganizationForIdentity(ctx,identityID,tenantID,organizationID);errors.Is(err,ErrOrganizationAccessDenied) {
		return nil,ErrOrganizationAccessDenied
	} else if err!=nil {
		return nil,err
	}
	rows,err:=s.DB.Query(ctx,`
		SELECT p.id,p.organization_id,p.tenant_id,p.property_code,p.name,
		       p.country_code,p.region_code,p.city_code,p.area_code,p.status,
		       CASE WHEN om.role IN ('owner','admin','manager') THEN om.role ELSE pm.role END
		FROM properties p
		JOIN organizations o ON o.id=p.organization_id AND o.tenant_id=p.tenant_id AND o.status='active'
		JOIN organization_memberships om
		  ON om.organization_id=p.organization_id AND om.tenant_id=p.tenant_id
		 AND om.identity_id=$1 AND om.status='active'
		JOIN identity_memberships im
		  ON im.identity_id=om.identity_id AND im.tenant_id=om.tenant_id
		 AND im.role=om.identity_role
		LEFT JOIN property_memberships pm
		  ON pm.property_id=p.id AND pm.organization_id=p.organization_id
		 AND pm.tenant_id=p.tenant_id AND pm.identity_id=om.identity_id
		 AND pm.identity_role=om.identity_role AND pm.status='active'
		 AND om.role IN ('member','viewer')
		WHERE p.organization_id=$2 AND p.tenant_id=$3 AND p.status='active'
		  AND (om.role IN ('owner','admin','manager') OR pm.id IS NOT NULL)
		ORDER BY p.property_code,p.id
	`,identityID,organizationID,tenantID)
	if err!=nil{return nil,err}
	defer rows.Close()
	items:=make([]PropertySummary,0)
	for rows.Next(){
		var item PropertySummary
		if err=rows.Scan(&item.ID,&item.OrganizationID,&item.TenantID,&item.Code,&item.Name,
			&item.CountryCode,&item.RegionCode,&item.CityCode,&item.AreaCode,&item.Status,&item.AccessRole);err!=nil{return nil,err}
		items=append(items,item)
	}
	if err=rows.Err();err!=nil{return nil,err}
	return items,nil
}

func (h *Handler) organizationTenant(r *http.Request) string {
	tenantID:=strings.TrimSpace(r.Header.Get("X-Tenant-ID"))
	if tenantID=="" { tenantID=strings.TrimSpace(h.DefaultTenantID) }
	return tenantID
}

func (h *Handler) organizationList(w http.ResponseWriter,r *http.Request) {
	principal,err:=h.principal(r)
	if err!=nil { writeError(w,http.StatusUnauthorized,"unauthorized");return }
	items,err:=h.Store.ListOrganizationsForIdentity(r.Context(),principal.IdentityID,h.organizationTenant(r))
	if err!=nil { writeError(w,http.StatusInternalServerError,"could not load organizations");return }
	writeJSON(w,http.StatusOK,map[string]any{"organizations":items})
}

func (h *Handler) organizationGet(w http.ResponseWriter,r *http.Request,organizationID string) {
	principal,err:=h.principal(r)
	if err!=nil { writeError(w,http.StatusUnauthorized,"unauthorized");return }
	item,err:=h.Store.GetOrganizationForIdentity(r.Context(),principal.IdentityID,h.organizationTenant(r),organizationID)
	if errors.Is(err,ErrOrganizationAccessDenied) { writeError(w,http.StatusNotFound,"organization not found");return }
	if err!=nil { writeError(w,http.StatusInternalServerError,"could not load organization");return }
	writeJSON(w,http.StatusOK,map[string]any{"organization":item})
}

func (h *Handler) organizationProperties(w http.ResponseWriter,r *http.Request,organizationID string) {
	principal,err:=h.principal(r)
	if err!=nil { writeError(w,http.StatusUnauthorized,"unauthorized");return }
	items,err:=h.Store.ListPropertiesForIdentity(r.Context(),principal.IdentityID,h.organizationTenant(r),organizationID)
	if errors.Is(err,ErrOrganizationAccessDenied) { writeError(w,http.StatusNotFound,"organization not found");return }
	if err!=nil { writeError(w,http.StatusInternalServerError,"could not load properties");return }
	writeJSON(w,http.StatusOK,map[string]any{"properties":items})
}

func parseOrganizationPath(path string) (string,bool) {
	parts:=strings.Split(strings.Trim(path,"/"),"/")
	if len(parts)<3 || parts[0]!="v1" || parts[1]!="organizations" || parts[2]=="" || len(parts[2])>200 {
		return "",false
	}
	if len(parts)==3 { return parts[2],true }
	if len(parts)==4 && parts[3]=="properties" { return parts[2],true }
	return "",false
}
