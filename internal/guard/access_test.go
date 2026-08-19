package guard

import "testing"

func TestAugmentAccessGrantsAddsAdministratorCatalogPermissions(t *testing.T) {
	access := Access{
		Roles: []Role{{Name: "Administrator", Source: BuiltInRoleSource}},
		Grants: []Grant{{
			Permission: PermissionGoalsRead,
			Scope:      Scope{Kind: ScopeOrganization, OrganizationID: "org-one", ResourceID: "org-one"},
		}},
	}
	AugmentAccessGrants("org-one", &access)
	seen := map[Permission]struct{}{}
	for _, grant := range access.Grants {
		if grant.Scope.Kind == ScopeOrganization {
			seen[grant.Permission] = struct{}{}
		}
	}
	for _, permission := range AdministratorBundlePermissions() {
		if _, ok := seen[permission]; !ok {
			t.Fatalf("missing augmented permission %q", permission)
		}
	}
}

func TestAugmentAccessGrantsIgnoresNonAdministratorRoles(t *testing.T) {
	access := Access{
		Roles: []Role{{Name: "Viewer", Source: LocalRoleSource}},
		Grants: []Grant{{
			Permission: PermissionGoalsRead,
			Scope:      Scope{Kind: ScopeOrganization, OrganizationID: "org-one", ResourceID: "org-one"},
		}},
	}
	AugmentAccessGrants("org-one", &access)
	if len(access.Grants) != 1 {
		t.Fatalf("expected grants unchanged, got %d", len(access.Grants))
	}
}
