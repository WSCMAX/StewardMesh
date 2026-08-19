package campusseed

import (
	"github.com/maxlemke/stewardmesh/internal/guard"
)

const campusDemoIssuer = "https://campus-demo.invalid/stewardmesh"

var campusDemoAssignmentSource = "oidc:" + stableID("guard-source", "campus-demo-v1")

type demoUserDef struct {
	Slug        string
	Username    string
	Password    string
	Email       string
	DisplayName string
	Description string
	RoleName    string
	Permissions []guard.Permission
	ScopeKind   guard.ScopeKind
	ScopeSlug   string
	Admin       bool
}

var campusDemoUsers = []demoUserDef{
	{
		Slug: "campus-admin", Username: "campus-admin",
		Password: "Campus demo admin 2026!", Email: "campus-admin@riverside-demo.invalid",
		DisplayName: "Campus Administrator", Description: "Full organization administrator with every permission.",
		RoleName: "Administrator", Admin: true,
	},
	{
		Slug: "it-director", Username: "it-director",
		Password: "IT director demo 2026!", Email: "it-director@riverside-demo.invalid",
		DisplayName: "Jordan Blake", Description: "IT Services director; manages assets, directory, integrations, and software.",
		RoleName: "IT Director",
		Permissions: []guard.Permission{
			guard.PermissionAssetsRead, guard.PermissionAssetsWrite,
			guard.PermissionDirectoryRead, guard.PermissionDirectoryWrite,
			guard.PermissionIntegrationsRead, guard.PermissionIntegrationsWrite,
			guard.PermissionSoftwareRead, guard.PermissionSoftwareWrite,
			guard.PermissionOrganizationRead,
		},
	},
	{
		Slug: "financial-aid-analyst", Username: "financial-aid-analyst",
		Password: "Financial aid demo 2026!", Email: "financial-aid@riverside-demo.invalid",
		DisplayName: "Taylor Nguyen", Description: "Financial Aid analyst; finance and directory read access for aid packaging.",
		RoleName: "Financial Aid Analyst",
		Permissions: []guard.Permission{
			guard.PermissionFinanceRead, guard.PermissionDirectoryRead,
			guard.PermissionAssetsRead, guard.PermissionOrganizationRead,
		},
	},
	{
		Slug: "lab-manager-arts", Username: "lab-manager-arts",
		Password: "Arts lab manager 2026!", Email: "lab-manager-arts@riverside-demo.invalid",
		DisplayName: "Morgan Chen", Description: "Arts & Humanities lab manager; department-scoped asset read/write for studio and design programs.",
		RoleName:    "Arts Lab Manager",
		Permissions: []guard.Permission{guard.PermissionAssetsRead, guard.PermissionAssetsWrite, guard.PermissionDirectoryRead},
		ScopeKind:   guard.ScopeDepartment, ScopeSlug: "studio-arts",
	},
	{
		Slug: "inventory-steward", Username: "inventory-steward",
		Password: "Inventory steward 2026!", Email: "inventory-steward@riverside-demo.invalid",
		DisplayName: "Alex Rivera", Description: "Organization-wide inventory steward; full asset read/write.",
		RoleName: "Inventory Steward",
		Permissions: []guard.Permission{
			guard.PermissionAssetsRead, guard.PermissionAssetsWrite,
			guard.PermissionDirectoryRead, guard.PermissionOrganizationRead,
		},
	},
	{
		Slug: "directory-registrar", Username: "directory-registrar",
		Password: "Registrar directory 2026!", Email: "registrar@riverside-demo.invalid",
		DisplayName: "Casey Brooks", Description: "Student Records registrar; directory read/write for roster maintenance.",
		RoleName: "Directory Registrar",
		Permissions: []guard.Permission{
			guard.PermissionDirectoryRead, guard.PermissionDirectoryWrite,
			guard.PermissionOrganizationRead,
		},
	},
	{
		Slug: "finance-controller", Username: "finance-controller",
		Password: "Finance controller 2026!", Email: "finance-controller@riverside-demo.invalid",
		DisplayName: "Riley Morgan", Description: "Finance controller; finance read/write for procurement and planning.",
		RoleName: "Finance Controller",
		Permissions: []guard.Permission{
			guard.PermissionFinanceRead, guard.PermissionFinanceWrite,
			guard.PermissionPlanningRead, guard.PermissionOrganizationRead,
		},
	},
	{
		Slug: "helpdesk-tech", Username: "helpdesk-tech",
		Password: "Helpdesk tech demo 2026!", Email: "helpdesk@riverside-demo.invalid",
		DisplayName: "Sam Ortiz", Description: "IT help desk technician; read-only asset and directory access plus messaging.",
		RoleName: "Help Desk Technician",
		Permissions: []guard.Permission{
			guard.PermissionAssetsRead, guard.PermissionDirectoryRead,
			guard.PermissionMessagingRead, guard.PermissionMessagingWrite,
			guard.PermissionOrganizationRead,
		},
	},
	{
		Slug: "dept-head-business", Username: "dept-head-business",
		Password: "Business dept head 2026!", Email: "dept-head-business@riverside-demo.invalid",
		DisplayName: "Dr. Priya Sharma", Description: "School of Business & Technology department head; department-scoped asset and directory read.",
		RoleName: "Business Department Head",
		Permissions: []guard.Permission{
			guard.PermissionAssetsRead, guard.PermissionDirectoryRead, guard.PermissionOrganizationRead,
		},
		ScopeKind: guard.ScopeDepartment, ScopeSlug: "school-business",
	},
	{
		Slug: "auditor-readonly", Username: "auditor-readonly",
		Password: "Auditor readonly 2026!", Email: "auditor@riverside-demo.invalid",
		DisplayName: "Jamie Foster", Description: "Read-only auditor across organization, assets, directory, finance, and planning.",
		RoleName: "Read-Only Auditor",
		Permissions: []guard.Permission{
			guard.PermissionOrganizationRead, guard.PermissionAssetsRead,
			guard.PermissionDirectoryRead, guard.PermissionFinanceRead,
			guard.PermissionPlanningRead, guard.PermissionSoftwareRead,
		},
	},
}
