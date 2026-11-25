package marketplace

import (
	"errors"
	"testing"
)

func TestAuthorizeRolePermissions(t *testing.T) {
	tests := []struct {
		name     string
		actor    Actor
		resource Resource
		action   Action
		allowed  bool
	}{
		{"customer creates order", Actor{"customer-1", "tenant-a", RoleCustomer}, Resource{TenantID: "tenant-a"}, ActionOrderCreate, true},
		{"customer reads own order", Actor{"customer-1", "tenant-a", RoleCustomer}, Resource{"tenant-a", "customer-1", "pro-1"}, ActionOrderRead, true},
		{"customer cannot read another order", Actor{"customer-1", "tenant-a", RoleCustomer}, Resource{"tenant-a", "customer-2", "pro-1"}, ActionOrderRead, false},
		{"customer cannot accept order", Actor{"customer-1", "tenant-a", RoleCustomer}, Resource{"tenant-a", "customer-1", "pro-1"}, ActionOrderAccept, false},
		{"professional reads assigned order", Actor{"pro-1", "tenant-a", RoleProfessional}, Resource{"tenant-a", "customer-1", "pro-1"}, ActionOrderRead, true},
		{"professional accepts own quote", Actor{"pro-1", "tenant-a", RoleProfessional}, Resource{TenantID:"tenant-a",OwnerID:"pro-1"}, ActionQuoteAccept, true},
		{"professional responds to own dispatch offer", Actor{"pro-1", "tenant-a", RoleProfessional}, Resource{TenantID:"tenant-a",OwnerID:"pro-1"}, ActionDispatchOfferRespond, true},
		{"professional cannot read unassigned order", Actor{"pro-1", "tenant-a", RoleProfessional}, Resource{"tenant-a", "customer-1", "pro-2"}, ActionOrderRead, false},
		{"operator can assign within tenant", Actor{"ops-1", "tenant-a", RoleOperator}, Resource{TenantID: "tenant-a"}, ActionOrderAssign, true},
		{"operator can configure market", Actor{"ops-1", "tenant-a", RoleOperator}, Resource{TenantID: "tenant-a"}, ActionMarketConfigure, true},
		{"operator can manage catalog", Actor{"ops-1", "tenant-a", RoleOperator}, Resource{TenantID: "tenant-a"}, ActionCatalogManage, true},
		{"support cannot configure market", Actor{"support-1", "tenant-a", RoleSupport}, Resource{TenantID: "tenant-a"}, ActionMarketConfigure, false},
		{"operator cannot post ledger", Actor{"ops-1", "tenant-a", RoleOperator}, Resource{TenantID: "tenant-a"}, ActionLedgerPost, false},
		{"support can read support case", Actor{"support-1", "tenant-a", RoleSupport}, Resource{TenantID: "tenant-a"}, ActionSupportCaseRead, true},
		{"support cannot read unrestricted case", Actor{"support-1", "tenant-a", RoleSupport}, Resource{TenantID: "tenant-a"}, ActionCaseRead, false},
		{"support cannot read identity documents", Actor{"support-1", "tenant-a", RoleSupport}, Resource{TenantID: "tenant-a"}, ActionIdentityRead, false},
		{"no human role posts ledger", Actor{"pro-1", "tenant-a", RoleProfessional}, Resource{TenantID: "tenant-a"}, ActionLedgerPost, false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := Authorize(test.actor, test.resource, test.action)
			if test.allowed && err != nil {
				t.Fatalf("expected access, got %v", err)
			}
			if !test.allowed && !errors.Is(err, ErrForbidden) {
				t.Fatalf("expected deny-by-default, got %v", err)
			}
		})
	}
}

func TestAuthorizeAlwaysRejectsCrossTenantAndMalformedPrincipals(t *testing.T) {
	tests := []struct {
		name  string
		actor Actor
		res   Resource
	}{
		{"cross tenant operator", Actor{"ops", "tenant-a", RoleOperator}, Resource{TenantID: "tenant-b"}},
		{"cross tenant support", Actor{"support", "tenant-a", RoleSupport}, Resource{TenantID: "tenant-b"}},
		{"empty subject", Actor{"", "tenant-a", RoleCustomer}, Resource{TenantID: "tenant-a", OwnerID: "customer"}},
		{"empty actor tenant", Actor{"customer", "", RoleCustomer}, Resource{TenantID: "tenant-a", OwnerID: "customer"}},
		{"empty resource tenant", Actor{"customer", "tenant-a", RoleCustomer}, Resource{OwnerID: "customer"}},
		{"unknown role", Actor{"root", "tenant-a", Role("root")}, Resource{TenantID: "tenant-a"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := Authorize(test.actor, test.res, ActionOrderRead); !errors.Is(err, ErrForbidden) {
				t.Fatalf("expected deny, got %v", err)
			}
		})
	}
}


func TestAuthorizeAnalyticsReadIsOperatorOnly(t *testing.T) {
    tests:=[]struct{name string; actor Actor; allowed bool}{
        {"operator",Actor{SubjectID:"operator",TenantID:"tenant-a",Role:RoleOperator},true},
        {"customer",Actor{SubjectID:"customer",TenantID:"tenant-a",Role:RoleCustomer},false},
        {"professional",Actor{SubjectID:"professional",TenantID:"tenant-a",Role:RoleProfessional},false},
        {"support",Actor{SubjectID:"support",TenantID:"tenant-a",Role:RoleSupport},false},
    }
    for _,tc:=range tests { t.Run(tc.name,func(t *testing.T){
        err:=Authorize(tc.actor,Resource{TenantID:"tenant-a"},ActionAnalyticsRead)
        if tc.allowed&&err!=nil { t.Fatalf("expected access, got %v",err) }
        if !tc.allowed&&!errors.Is(err,ErrForbidden) { t.Fatalf("expected deny, got %v",err) }
    }) }
}
