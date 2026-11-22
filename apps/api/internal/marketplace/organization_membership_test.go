package marketplace

import "testing"

func TestOrganizationMembershipManagementPolicy(t *testing.T) {
	tests := []struct {
		name, actor, target, next string
		allowed bool
	}{
		{"owner can assign owner", "owner", "member", "owner", true},
		{"owner can manage owner", "owner", "owner", "admin", true},
		{"admin can assign manager", "admin", "member", "manager", true},
		{"admin cannot assign owner", "admin", "member", "owner", false},
		{"admin cannot manage owner", "admin", "owner", "admin", false},
		{"manager can promote viewer to member", "manager", "viewer", "member", true},
		{"manager cannot assign manager", "manager", "member", "manager", false},
		{"manager cannot manage admin", "manager", "admin", "viewer", false},
		{"member cannot manage members", "member", "viewer", "member", false},
		{"viewer cannot manage members", "viewer", "member", "viewer", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := membershipManagerCanManage(tt.actor, tt.target, tt.next); got != tt.allowed {
				t.Fatalf("membershipManagerCanManage(%q,%q,%q) = %v, want %v",
					tt.actor, tt.target, tt.next, got, tt.allowed)
			}
		})
	}
}

func TestValidOrganizationRole(t *testing.T) {
	for _, role := range []string{"owner", "admin", "manager", "member", "viewer"} {
		if !validOrganizationRole(role) {
			t.Errorf("valid role %q rejected", role)
		}
	}
	for _, role := range []string{"", "operator", "support", "customer", "OWNER"} {
		if validOrganizationRole(role) {
			t.Errorf("invalid organization role %q accepted", role)
		}
	}
}
