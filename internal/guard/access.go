package guard

import (
	"sort"
	"strings"
)

// AugmentAccessGrants applies the current built-in Administrator permission
// catalog to organization-scoped grants. Older databases may not yet include
// every permission row added after initial bootstrap.
func AugmentAccessGrants(organizationID string, access *Access) {
	if access == nil || organizationID == "" {
		return
	}
	hasAdministrator := false
	for _, role := range access.Roles {
		if role.Source == BuiltInRoleSource && strings.EqualFold(strings.TrimSpace(role.Name), "administrator") {
			hasAdministrator = true
			break
		}
	}
	if !hasAdministrator {
		return
	}
	scope := Scope{Kind: ScopeOrganization, OrganizationID: organizationID, ResourceID: organizationID}
	seen := make(map[Permission]struct{}, len(access.Grants))
	for _, grant := range access.Grants {
		if grant.Scope.Kind == ScopeOrganization && grant.Scope.OrganizationID == organizationID {
			seen[grant.Permission] = struct{}{}
		}
	}
	for _, permission := range AdministratorBundlePermissions() {
		if _, exists := seen[permission]; exists {
			continue
		}
		seen[permission] = struct{}{}
		access.Grants = append(access.Grants, Grant{Permission: permission, Scope: scope})
	}
	sort.Slice(access.Grants, func(i, j int) bool {
		if access.Grants[i].Permission == access.Grants[j].Permission {
			return access.Grants[i].Scope.ResourceID < access.Grants[j].Scope.ResourceID
		}
		return access.Grants[i].Permission < access.Grants[j].Permission
	})
}
