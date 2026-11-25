package marketplace

import "errors"

type Role string
type Action string

const (
	RoleCustomer     Role = "customer"
	RoleProfessional Role = "professional"
	RoleOperator     Role = "operator"
	RoleSupport      Role = "support"
)

const (
	ActionOrderCreate       Action = "order:create"
	ActionOrderRead         Action = "order:read"
	ActionOrderCancel       Action = "order:cancel"
	ActionOrderAccept       Action = "order:accept"
	ActionOrderStart        Action = "order:start"
	ActionOrderComplete     Action = "order:complete"
	ActionOrderApproveCompletion Action = "order:completion:approve"
	ActionOrderAssign       Action = "order:assign"
	ActionQuoteApprove      Action = "quote:approve"
	ActionQuoteAccept       Action = "quote:accept"
	ActionDispatchOfferRespond Action = "dispatch:offer:respond"
	ActionProfileUpdate     Action = "profile:update"
	ActionProviderVerify    Action = "provider:verify"
	ActionCaseRead          Action = "case:read"
	ActionSupportCaseRead   Action = "support:case:read"
	ActionSupportCaseComment Action = "support:case:comment"
	ActionLedgerPost        Action = "ledger:post"
	ActionIdentityRead      Action = "identity:read_sensitive"
	ActionMarketConfigure Action = "market:configure"
	ActionCatalogManage Action = "catalog:manage"
	ActionPolicyManage Action = "policy:manage"
	ActionAnalyticsRead Action = "analytics:read"
	ActionOperatorPermissionsManage Action = "operator:permissions:manage"
)

var ErrForbidden = errors.New("access denied")

type Actor struct {
	SubjectID string
	TenantID  string
	Role      Role
}

type Resource struct {
	TenantID                string
	OwnerID                 string
	AssignedProfessionalID  string
}

func validRole(role Role) bool {
	switch role {
	case RoleCustomer, RoleProfessional, RoleOperator, RoleSupport:
		return true
	default:
		return false
	}
}

// Authorize is deliberately deny-by-default. Tenant isolation is checked before
// role policy, so even operator/support roles never cross tenant boundaries.
func Authorize(actor Actor, resource Resource, action Action) error {
	if actor.SubjectID == "" || actor.TenantID == "" || resource.TenantID == "" || !validRole(actor.Role) {
		return ErrForbidden
	}
	if actor.TenantID != resource.TenantID {
		return ErrForbidden
	}

	allowed := false
	switch actor.Role {
	case RoleCustomer:
		switch action {
		case ActionOrderCreate:
			allowed = true
		case ActionOrderRead, ActionOrderCancel, ActionQuoteApprove, ActionOrderApproveCompletion:
			allowed = resource.OwnerID != "" && resource.OwnerID == actor.SubjectID
		}
	case RoleProfessional:
		switch action {
		case ActionOrderRead, ActionOrderAccept, ActionOrderStart, ActionOrderComplete:
			allowed = resource.AssignedProfessionalID != "" && resource.AssignedProfessionalID == actor.SubjectID
		case ActionProfileUpdate, ActionQuoteAccept, ActionDispatchOfferRespond:
			allowed = resource.OwnerID != "" && resource.OwnerID == actor.SubjectID
		}
	case RoleOperator:
		switch action {
		case ActionOrderRead, ActionOrderAssign, ActionProviderVerify, ActionCaseRead, ActionMarketConfigure, ActionCatalogManage, ActionPolicyManage, ActionAnalyticsRead:
			allowed = true
		}
	case RoleSupport:
		switch action {
		case ActionSupportCaseRead, ActionSupportCaseComment:
			allowed = true
		}
	}
	if !allowed {
		return ErrForbidden
	}
	return nil
}
