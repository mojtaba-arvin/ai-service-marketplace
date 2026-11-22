package marketplace

import (
	"errors"
	"net/http"
	"strings"
)

func (h *Handler) organizationManagementRoutes(w http.ResponseWriter,r *http.Request,organizationID string) bool {
	principal,err:=h.principal(r)
	if err!=nil{writeError(w,http.StatusUnauthorized,"unauthorized");return true}
	switch r.Method {
	case http.MethodPatch:
		var in UpdateOrganizationInput
		if err=decodeOrganizationInput(r,&in);err!=nil{writeError(w,http.StatusBadRequest,"invalid organization payload");return true}
		item,e:=h.Store.UpdateOrganizationForIdentity(r.Context(),principal.IdentityID,h.organizationTenant(r),organizationID,in)
		if e!=nil{writeOrganizationManagementError(w,e);return true}
		writeJSON(w,http.StatusOK,map[string]any{"organization":item})
		return true
	case http.MethodDelete:
		e:=h.Store.ArchiveOrganizationForIdentity(r.Context(),principal.IdentityID,h.organizationTenant(r),organizationID)
		if e!=nil{writeOrganizationManagementError(w,e);return true}
		writeJSON(w,http.StatusOK,map[string]string{"status":"archived"})
		return true
	default:
		return false
	}
}

func (h *Handler) organizationPropertyManagementRoutes(w http.ResponseWriter,r *http.Request,organizationID string) bool {
	principal,err:=h.principal(r)
	if err!=nil{writeError(w,http.StatusUnauthorized,"unauthorized");return true}
	parts:=strings.Split(strings.Trim(r.URL.Path,"/"),"/")
	if len(parts)<5||parts[0]!="v1"||parts[1]!="organizations"||parts[2]!=organizationID||parts[3]!="properties"||parts[4]==""||len(parts[4])>200{
		writeError(w,http.StatusNotFound,"not found");return true
	}
	tenantID:=h.organizationTenant(r)
	propertyID:=parts[4]
	if len(parts)==5 {
		switch r.Method {
		case http.MethodPatch:
			var in UpdatePropertyInput
			if err=decodeOrganizationInput(r,&in);err!=nil{writeError(w,http.StatusBadRequest,"invalid property payload");return true}
			item,e:=h.Store.UpdatePropertyForIdentity(r.Context(),principal.IdentityID,tenantID,organizationID,propertyID,in)
			if e!=nil{writeOrganizationManagementError(w,e);return true}
			writeJSON(w,http.StatusOK,map[string]any{"property":item})
			return true
		case http.MethodDelete:
			e:=h.Store.ArchivePropertyForIdentity(r.Context(),principal.IdentityID,tenantID,organizationID,propertyID)
			if e!=nil{writeOrganizationManagementError(w,e);return true}
			writeJSON(w,http.StatusOK,map[string]string{"status":"archived"})
			return true
		}
	}
	if len(parts)==6&&parts[5]=="memberships" {
		switch r.Method {
		case http.MethodGet:
			items,e:=h.Store.ListPropertyDelegationsForIdentity(r.Context(),principal.IdentityID,tenantID,organizationID,propertyID)
			if e!=nil{writeOrganizationManagementError(w,e);return true}
			writeJSON(w,http.StatusOK,map[string]any{"memberships":items})
			return true
		case http.MethodPost:
			var in PropertyDelegationInput
			if err=decodeOrganizationInput(r,&in);err!=nil{writeError(w,http.StatusBadRequest,"invalid property membership payload");return true}
			item,e:=h.Store.GrantPropertyDelegation(r.Context(),principal.IdentityID,tenantID,organizationID,propertyID,in)
			if e!=nil{writeOrganizationManagementError(w,e);return true}
			writeJSON(w,http.StatusCreated,map[string]any{"membership":item})
			return true
		}
	}
	if len(parts)==7&&parts[5]=="memberships"&&parts[6]!=""&&r.Method==http.MethodDelete {
		e:=h.Store.RevokePropertyDelegation(r.Context(),principal.IdentityID,tenantID,organizationID,propertyID,parts[6])
		if e!=nil{writeOrganizationManagementError(w,e);return true}
		writeJSON(w,http.StatusOK,map[string]string{"status":"revoked"})
		return true
	}
	if len(parts)==8&&parts[5]=="memberships"&&parts[6]!=""&&parts[7]=="role"&&r.Method==http.MethodPatch {
		var in struct{Role string `json:"role"`}
		if err=decodeOrganizationInput(r,&in);err!=nil{writeError(w,http.StatusBadRequest,"invalid role payload");return true}
		item,e:=h.Store.ChangePropertyDelegationRole(r.Context(),principal.IdentityID,tenantID,organizationID,propertyID,parts[6],in.Role)
		if e!=nil{writeOrganizationManagementError(w,e);return true}
		writeJSON(w,http.StatusOK,map[string]any{"membership":item})
		return true
	}
	return false
}

func writeOrganizationManagementError(w http.ResponseWriter,err error) {
	switch {
	case errors.Is(err,ErrMembershipForbidden):
		writeError(w,http.StatusForbidden,"organization or property operation forbidden")
	case errors.Is(err,ErrMembershipNotFound),errors.Is(err,ErrOrganizationWriteForbidden):
		writeError(w,http.StatusNotFound,"organization, property or membership not found")
	case errors.Is(err,ErrMembershipConflict):
		writeError(w,http.StatusConflict,"organization or property state conflict")
	case errors.Is(err,ErrOrganizationInput):
		writeError(w,http.StatusBadRequest,"invalid organization or property payload")
	case isOrganizationConflict(err):
		writeError(w,http.StatusConflict,"organization slug or property code already exists")
	default:
		writeError(w,http.StatusInternalServerError,"organization or property operation failed")
	}
}
