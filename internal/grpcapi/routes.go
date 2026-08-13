// Requirements: REQ-API-001, REQ-SIGNALS-001. Feature: integrations.protocols.
package grpcapi

import "net/http"

type requestKind uint8

const (
	requestJSON requestKind = iota
	requestRaw
	requestMultipart
)

type responseKind uint8

const (
	responseJSON responseKind = iota
	responseCSV
	responseExchangeExport
	responseAssetLabel
	responseVaultDownload
	responseDeleted
)

type route struct {
	method       string
	path         string
	public       bool
	pathFields   map[string]string
	queryFields  map[string]string
	headerFields map[string]string
	flatten      []string
	requestKind  requestKind
	rawBodyField string
	contentType  string
	responseKind responseKind
}

func endpoint(method, path string) route { return route{method: method, path: path} }

func routes() map[string]route {
	result := map[string]route{
		"/stewardmesh.v1.FoundationService/GetOrganization": endpoint(http.MethodGet, "/api/v1/organization"),

		"/stewardmesh.v1.BridgeService/ListClients":  endpoint(http.MethodGet, "/api/v1/bridge/clients"),
		"/stewardmesh.v1.BridgeService/CreateClient": endpoint(http.MethodPost, "/api/v1/bridge/clients"),
		"/stewardmesh.v1.BridgeService/RevokeClient": endpoint(http.MethodDelete, "/api/v1/bridge/clients/{clientId}"),
		"/stewardmesh.v1.BridgeService/ListGrants":   endpoint(http.MethodGet, "/api/v1/bridge/grants"),
		"/stewardmesh.v1.BridgeService/RevokeGrant":  endpoint(http.MethodDelete, "/api/v1/bridge/grants/{grantId}"),

		"/stewardmesh.v1.PatternsService/ListTemplates":         endpoint(http.MethodGet, "/api/v1/templates"),
		"/stewardmesh.v1.PatternsService/CreateTemplate":        endpoint(http.MethodPost, "/api/v1/templates"),
		"/stewardmesh.v1.PatternsService/GetTemplate":           endpoint(http.MethodGet, "/api/v1/templates/{templateId}"),
		"/stewardmesh.v1.PatternsService/GetTemplateSchema":     endpoint(http.MethodGet, "/api/v1/templates/{templateId}/schema"),
		"/stewardmesh.v1.PatternsService/CopyTemplate":          {method: http.MethodPost, path: "/api/v1/templates/{sourceTemplateId}/copy", queryFields: map[string]string{"sourceVersion": "version"}},
		"/stewardmesh.v1.PatternsService/CreateTemplateVersion": endpoint(http.MethodPost, "/api/v1/templates/{templateId}/versions"),
		"/stewardmesh.v1.PatternsService/ValidateRecord":        {method: http.MethodPost, path: "/api/v1/templates/{templateId}/validate", queryFields: map[string]string{"version": "version"}},
		"/stewardmesh.v1.PatternsService/ExportCSVTemplate":     {method: http.MethodGet, path: "/api/v1/templates/{templateId}/template.csv", queryFields: map[string]string{"version": "version"}, responseKind: responseCSV},

		"/stewardmesh.v1.GuardService/GetBootstrapStatus":        {method: http.MethodGet, path: "/api/v1/auth/bootstrap", public: true},
		"/stewardmesh.v1.GuardService/BootstrapAdministrator":    {method: http.MethodPost, path: "/api/v1/auth/bootstrap", public: true},
		"/stewardmesh.v1.GuardService/AuthenticateLocal":         {method: http.MethodPost, path: "/api/v1/auth/login", public: true},
		"/stewardmesh.v1.GuardService/GetSession":                endpoint(http.MethodGet, "/api/v1/auth/session"),
		"/stewardmesh.v1.GuardService/Logout":                    endpoint(http.MethodPost, "/api/v1/auth/logout"),
		"/stewardmesh.v1.GuardService/ListGuardAccess":           endpoint(http.MethodGet, "/api/v1/guard/access"),
		"/stewardmesh.v1.GuardService/CreateRole":                endpoint(http.MethodPost, "/api/v1/guard/roles"),
		"/stewardmesh.v1.GuardService/CreateRoleAssignment":      endpoint(http.MethodPost, "/api/v1/guard/role-assignments"),
		"/stewardmesh.v1.GuardService/DeleteRoleAssignment":      endpoint(http.MethodDelete, "/api/v1/guard/role-assignments/{assignmentId}"),
		"/stewardmesh.v1.GuardService/ListResourceOwnership":     endpoint(http.MethodGet, "/api/v1/guard/resource-ownership"),
		"/stewardmesh.v1.GuardService/RegisterResourceOwnership": endpoint(http.MethodPost, "/api/v1/guard/resource-ownership"),
		"/stewardmesh.v1.GuardService/ClaimResourceOwnership":    endpoint(http.MethodPost, "/api/v1/guard/resource-ownership/{resourceType}/{resourceId}/claim"),

		"/stewardmesh.v1.AssetService/ListAssetModels":           endpoint(http.MethodGet, "/api/v1/asset-models"),
		"/stewardmesh.v1.AssetService/GetAssetModel":             endpoint(http.MethodGet, "/api/v1/asset-models/{modelId}"),
		"/stewardmesh.v1.AssetService/GetAssetModelInventory":    endpoint(http.MethodGet, "/api/v1/asset-models/{modelId}/inventory"),
		"/stewardmesh.v1.AssetService/ResolveAssetModel":         endpoint(http.MethodGet, "/api/v1/asset-models/resolve"),
		"/stewardmesh.v1.AssetService/CreateAssetModel":          {method: http.MethodPost, path: "/api/v1/asset-models", flatten: []string{"model"}},
		"/stewardmesh.v1.AssetService/UpdateAssetModel":          {method: http.MethodPut, path: "/api/v1/asset-models/{id}", pathFields: map[string]string{"id": "model.id"}, flatten: []string{"model"}},
		"/stewardmesh.v1.AssetService/RetireAssetModel":          {method: http.MethodPost, path: "/api/v1/asset-models/{modelId}/retire", queryFields: map[string]string{"revision": "revision"}},
		"/stewardmesh.v1.AssetService/ListAssets":                endpoint(http.MethodGet, "/api/v1/assets"),
		"/stewardmesh.v1.AssetService/GetAsset":                  endpoint(http.MethodGet, "/api/v1/assets/{assetId}"),
		"/stewardmesh.v1.AssetService/CreateAsset":               {method: http.MethodPost, path: "/api/v1/assets", flatten: []string{"asset"}},
		"/stewardmesh.v1.AssetService/CreateAssetsFromModel":     endpoint(http.MethodPost, "/api/v1/asset-models/{modelId}/assets/bulk"),
		"/stewardmesh.v1.AssetService/UpdateAsset":               {method: http.MethodPut, path: "/api/v1/assets/{id}", pathFields: map[string]string{"id": "asset.id"}, flatten: []string{"asset"}},
		"/stewardmesh.v1.AssetService/ListAssetLifecycle":        endpoint(http.MethodGet, "/api/v1/assets/{assetId}/lifecycle"),
		"/stewardmesh.v1.AssetService/ResolveAssetIdentifier":    endpoint(http.MethodPost, "/api/v1/asset-identifiers/resolve"),
		"/stewardmesh.v1.AssetService/ListAssetIdentifiers":      endpoint(http.MethodGet, "/api/v1/assets/{assetId}/identifiers"),
		"/stewardmesh.v1.AssetService/CreateAssetIdentifier":     endpoint(http.MethodPost, "/api/v1/assets/{assetId}/identifiers"),
		"/stewardmesh.v1.AssetService/ReplaceAssetIdentifier":    endpoint(http.MethodPost, "/api/v1/assets/{assetId}/identifiers/{identifierId}/replace"),
		"/stewardmesh.v1.AssetService/DeactivateAssetIdentifier": endpoint(http.MethodPost, "/api/v1/assets/{assetId}/identifiers/{identifierId}/deactivate"),
		"/stewardmesh.v1.AssetService/ListAssetLabelTemplates":   endpoint(http.MethodGet, "/api/v1/asset-label-templates"),
		"/stewardmesh.v1.AssetService/GenerateAssetLabelBatch":   {method: http.MethodPost, path: "/api/v1/asset-label-batches", headerFields: map[string]string{"idempotencyKey": "Idempotency-Key"}, responseKind: responseAssetLabel},

		"/stewardmesh.v1.PeopleService/ListSites":             endpoint(http.MethodGet, "/api/v1/sites"),
		"/stewardmesh.v1.PeopleService/CreateSite":            endpoint(http.MethodPost, "/api/v1/sites"),
		"/stewardmesh.v1.PeopleService/ListBuildings":         endpoint(http.MethodGet, "/api/v1/buildings"),
		"/stewardmesh.v1.PeopleService/CreateBuilding":        endpoint(http.MethodPost, "/api/v1/buildings"),
		"/stewardmesh.v1.PeopleService/ListRooms":             endpoint(http.MethodGet, "/api/v1/rooms"),
		"/stewardmesh.v1.PeopleService/CreateRoom":            endpoint(http.MethodPost, "/api/v1/rooms"),
		"/stewardmesh.v1.PeopleService/ListDepartments":       endpoint(http.MethodGet, "/api/v1/departments"),
		"/stewardmesh.v1.PeopleService/CreateDepartment":      endpoint(http.MethodPost, "/api/v1/departments"),
		"/stewardmesh.v1.PeopleService/SearchIdentities":      endpoint(http.MethodGet, "/api/v1/identities"),
		"/stewardmesh.v1.PeopleService/CreateIdentity":        endpoint(http.MethodPost, "/api/v1/identities"),
		"/stewardmesh.v1.PeopleService/ListAssetAssignments":  endpoint(http.MethodGet, "/api/v1/assets/{assetId}/assignments"),
		"/stewardmesh.v1.PeopleService/CreateAssetAssignment": endpoint(http.MethodPost, "/api/v1/assets/{assetId}/assignments"),
		"/stewardmesh.v1.PeopleService/EndAssetAssignment":    endpoint(http.MethodPatch, "/api/v1/assets/{assetId}/assignments/{assignmentId}"),

		"/stewardmesh.v1.RelationshipGraphService/GetRelationshipGraph": endpoint(http.MethodGet, "/api/v1/graph"),

		"/stewardmesh.v1.DirectoryImportService/ListDirectoryImportSources": endpoint(http.MethodGet, "/api/v1/directory-import-sources"),
		"/stewardmesh.v1.DirectoryImportService/ListDirectoryImports":       endpoint(http.MethodGet, "/api/v1/directory-imports"),
		"/stewardmesh.v1.DirectoryImportService/GetDirectoryImport":         endpoint(http.MethodGet, "/api/v1/directory-imports/{batchId}"),
		"/stewardmesh.v1.DirectoryImportService/PreviewDirectoryImport":     {method: http.MethodPost, path: "/api/v1/directory-imports/preview", headerFields: map[string]string{"idempotencyKey": "Idempotency-Key"}},
		"/stewardmesh.v1.DirectoryImportService/ApplyDirectoryImport":       {method: http.MethodPost, path: "/api/v1/directory-imports/{batchId}/apply", headerFields: map[string]string{"idempotencyKey": "Idempotency-Key"}},
		"/stewardmesh.v1.DirectoryImportService/RetryDirectoryImport":       {method: http.MethodPost, path: "/api/v1/directory-imports/{batchId}/retry", headerFields: map[string]string{"idempotencyKey": "Idempotency-Key"}},

		"/stewardmesh.v1.ThreadsService/ListTags":          endpoint(http.MethodGet, "/api/v1/tags"),
		"/stewardmesh.v1.ThreadsService/GetTag":            endpoint(http.MethodGet, "/api/v1/tags/{tagId}"),
		"/stewardmesh.v1.ThreadsService/CreateTag":         endpoint(http.MethodPost, "/api/v1/tags"),
		"/stewardmesh.v1.ThreadsService/UpdateTag":         endpoint(http.MethodPut, "/api/v1/tags/{tagId}"),
		"/stewardmesh.v1.ThreadsService/ListGoals":         endpoint(http.MethodGet, "/api/v1/goals"),
		"/stewardmesh.v1.ThreadsService/GetGoal":           endpoint(http.MethodGet, "/api/v1/goals/{goalId}"),
		"/stewardmesh.v1.ThreadsService/CreateGoal":        endpoint(http.MethodPost, "/api/v1/goals"),
		"/stewardmesh.v1.ThreadsService/UpdateGoal":        endpoint(http.MethodPut, "/api/v1/goals/{goalId}"),
		"/stewardmesh.v1.ThreadsService/ListEffectiveTags": endpoint(http.MethodGet, "/api/v1/threads/{targetType}/{targetId}/tags"),
		"/stewardmesh.v1.ThreadsService/SetTagRule":        endpoint(http.MethodPut, "/api/v1/threads/{targetType}/{targetId}/tags/{tagId}"),
		"/stewardmesh.v1.ThreadsService/DeleteTagRule":     endpoint(http.MethodDelete, "/api/v1/threads/{targetType}/{targetId}/tags/{tagId}"),
		"/stewardmesh.v1.ThreadsService/ListGoalLinks":     endpoint(http.MethodGet, "/api/v1/threads/{targetType}/{targetId}/goals"),
		"/stewardmesh.v1.ThreadsService/LinkGoal":          endpoint(http.MethodPut, "/api/v1/threads/{targetType}/{targetId}/goals/{goalId}"),
		"/stewardmesh.v1.ThreadsService/UnlinkGoal":        endpoint(http.MethodDelete, "/api/v1/threads/{targetType}/{targetId}/goals/{goalId}"),

		"/stewardmesh.v1.VaultService/ListBlobs":         endpoint(http.MethodGet, "/api/v1/blobs"),
		"/stewardmesh.v1.VaultService/GetBlob":           endpoint(http.MethodGet, "/api/v1/blobs/{blobId}"),
		"/stewardmesh.v1.VaultService/CreateBlob":        {method: http.MethodPost, path: "/api/v1/blobs", requestKind: requestMultipart},
		"/stewardmesh.v1.VaultService/DownloadBlob":      {method: http.MethodGet, path: "/api/v1/blobs/{blobId}/content", responseKind: responseVaultDownload},
		"/stewardmesh.v1.VaultService/AuthorizeDownload": endpoint(http.MethodPost, "/api/v1/blobs/{blobId}/download-authorization"),

		"/stewardmesh.v1.HorizonService/ListPlans":       endpoint(http.MethodGet, "/api/v1/horizon/plans"),
		"/stewardmesh.v1.HorizonService/CreatePlan":      endpoint(http.MethodPost, "/api/v1/horizon/plans"),
		"/stewardmesh.v1.HorizonService/UpdatePlan":      endpoint(http.MethodPut, "/api/v1/horizon/plans/{planId}"),
		"/stewardmesh.v1.HorizonService/ListPlanHistory": endpoint(http.MethodGet, "/api/v1/horizon/plans/{planId}/history"),
		"/stewardmesh.v1.HorizonService/GetForecast":     endpoint(http.MethodGet, "/api/v1/horizon/forecast"),
		"/stewardmesh.v1.HorizonService/ExportCSV":       {method: http.MethodGet, path: "/api/v1/horizon/export.csv", responseKind: responseCSV},

		"/stewardmesh.v1.LedgerService/GetSnapshot":               endpoint(http.MethodGet, "/api/v1/ledger"),
		"/stewardmesh.v1.LedgerService/CreateVendor":              endpoint(http.MethodPost, "/api/v1/ledger/vendors"),
		"/stewardmesh.v1.LedgerService/CreatePurchaseOrder":       endpoint(http.MethodPost, "/api/v1/ledger/purchase-orders"),
		"/stewardmesh.v1.LedgerService/UpdatePurchaseOrderStatus": endpoint(http.MethodPut, "/api/v1/ledger/purchase-orders/{purchaseOrderId}/status"),
		"/stewardmesh.v1.LedgerService/CreateContract":            endpoint(http.MethodPost, "/api/v1/ledger/contracts"),
		"/stewardmesh.v1.LedgerService/UpdateContractStatus":      endpoint(http.MethodPut, "/api/v1/ledger/contracts/{contractId}/status"),
		"/stewardmesh.v1.LedgerService/CreateCommitment":          endpoint(http.MethodPost, "/api/v1/ledger/commitments"),
		"/stewardmesh.v1.LedgerService/CreateBudget":              endpoint(http.MethodPost, "/api/v1/ledger/budgets"),
		"/stewardmesh.v1.LedgerService/ReconcileCost":             endpoint(http.MethodPost, "/api/v1/ledger/costs/reconcile"),
		"/stewardmesh.v1.LedgerService/GetBudgetVariance":         endpoint(http.MethodGet, "/api/v1/ledger/budget-variance"),
		"/stewardmesh.v1.LedgerService/ExportCSV":                 {method: http.MethodGet, path: "/api/v1/ledger/export.csv", responseKind: responseCSV},

		"/stewardmesh.v1.StackService/GetSnapshot":              endpoint(http.MethodGet, "/api/v1/stack"),
		"/stewardmesh.v1.StackService/GetAnalytics":             endpoint(http.MethodGet, "/api/v1/stack/analytics"),
		"/stewardmesh.v1.StackService/CreateProduct":            endpoint(http.MethodPost, "/api/v1/stack/products"),
		"/stewardmesh.v1.StackService/UpdateProductStatus":      endpoint(http.MethodPut, "/api/v1/stack/products/{productId}/status"),
		"/stewardmesh.v1.StackService/CreateVersion":            endpoint(http.MethodPost, "/api/v1/stack/versions"),
		"/stewardmesh.v1.StackService/UpdateVersionStatus":      endpoint(http.MethodPut, "/api/v1/stack/versions/{versionId}/status"),
		"/stewardmesh.v1.StackService/RecordInstallation":       endpoint(http.MethodPost, "/api/v1/stack/installations"),
		"/stewardmesh.v1.StackService/UpdateInstallationState":  endpoint(http.MethodPut, "/api/v1/stack/installations/{installationId}"),
		"/stewardmesh.v1.StackService/CreateLicense":            endpoint(http.MethodPost, "/api/v1/stack/licenses"),
		"/stewardmesh.v1.StackService/UpdateLicenseEntitlement": endpoint(http.MethodPut, "/api/v1/stack/licenses/{licenseId}/entitlement"),
		"/stewardmesh.v1.StackService/CreateAssignment":         endpoint(http.MethodPost, "/api/v1/stack/assignments"),
		"/stewardmesh.v1.StackService/UpdateAssignmentUsage":    endpoint(http.MethodPut, "/api/v1/stack/assignments/{assignmentId}/usage"),
		"/stewardmesh.v1.StackService/EndAssignment":            endpoint(http.MethodPut, "/api/v1/stack/assignments/{assignmentId}/end"),
		"/stewardmesh.v1.StackService/ExportRecords":            endpoint(http.MethodGet, "/api/v1/stack/exchange"),
		"/stewardmesh.v1.StackService/ImportRecords":            endpoint(http.MethodPost, "/api/v1/stack/exchange/import"),

		"/stewardmesh.v1.SignalsService/ListRules":               endpoint(http.MethodGet, "/api/v1/signals/rules"),
		"/stewardmesh.v1.SignalsService/CreateRule":              endpoint(http.MethodPost, "/api/v1/signals/rules"),
		"/stewardmesh.v1.SignalsService/UpdateRule":              endpoint(http.MethodPut, "/api/v1/signals/rules/{ruleId}"),
		"/stewardmesh.v1.SignalsService/ListAlerts":              endpoint(http.MethodGet, "/api/v1/signals/alerts"),
		"/stewardmesh.v1.SignalsService/ListAlertHistory":        endpoint(http.MethodGet, "/api/v1/signals/alerts/{alertId}/history"),
		"/stewardmesh.v1.SignalsService/Evaluate":                endpoint(http.MethodPost, "/api/v1/signals/evaluate"),
		"/stewardmesh.v1.SignalsService/AcknowledgeAlert":        endpoint(http.MethodPost, "/api/v1/signals/alerts/{alertId}/acknowledge"),
		"/stewardmesh.v1.SignalsService/AssignAlert":             endpoint(http.MethodPut, "/api/v1/signals/alerts/{alertId}/assignment"),
		"/stewardmesh.v1.SignalsService/ListSubscriptions":       endpoint(http.MethodGet, "/api/v1/signals/subscriptions"),
		"/stewardmesh.v1.SignalsService/ListSubscriptionTargets": endpoint(http.MethodGet, "/api/v1/signals/subscription-targets"),
		"/stewardmesh.v1.SignalsService/CreateSubscription":      endpoint(http.MethodPost, "/api/v1/signals/subscriptions"),
		"/stewardmesh.v1.SignalsService/DeleteSubscription":      {method: http.MethodDelete, path: "/api/v1/signals/subscriptions/{subscriptionId}", responseKind: responseDeleted},
		"/stewardmesh.v1.SignalsService/ListPendingDeliveries":   endpoint(http.MethodGet, "/api/v1/signals/deliveries/pending"),
		"/stewardmesh.v1.SignalsService/RecordDeliveryAttempt":   endpoint(http.MethodPost, "/api/v1/signals/deliveries/{deliveryId}/attempts"),
		"/stewardmesh.v1.SignalsService/ExportCSV":               {method: http.MethodGet, path: "/api/v1/signals/report.csv", responseKind: responseCSV},

		"/stewardmesh.v1.ExchangeService/ListExchangeRecords":   endpoint(http.MethodGet, "/api/v1/exchange/records"),
		"/stewardmesh.v1.ExchangeService/ListExchangePackages":  endpoint(http.MethodGet, "/api/v1/exchange/packages"),
		"/stewardmesh.v1.ExchangeService/ExportExchangePackage": {method: http.MethodPost, path: "/api/v1/exchange/export", responseKind: responseExchangeExport},
		"/stewardmesh.v1.ExchangeService/ImportExchangePackage": {method: http.MethodPost, path: "/api/v1/exchange/import", requestKind: requestRaw, rawBodyField: "archive", contentType: "application/vnd.stewardmesh.openinventory+zip"},

		"/stewardmesh.v1.ReachService/ListEndpoints":        endpoint(http.MethodGet, "/api/v1/reach/endpoints"),
		"/stewardmesh.v1.ReachService/ListProviders":        endpoint(http.MethodGet, "/api/v1/reach/providers"),
		"/stewardmesh.v1.ReachService/CreateProvider":       endpoint(http.MethodPost, "/api/v1/reach/providers"),
		"/stewardmesh.v1.ReachService/UpdateProvider":       endpoint(http.MethodPut, "/api/v1/reach/providers/{providerId}"),
		"/stewardmesh.v1.ReachService/RotateProviderSecret": endpoint(http.MethodPost, "/api/v1/reach/providers/{providerId}/rotate-secret"),
		"/stewardmesh.v1.ReachService/TestProvider":         endpoint(http.MethodPost, "/api/v1/reach/providers/{providerId}/test"),
		"/stewardmesh.v1.ReachService/ListProviderTests":    endpoint(http.MethodGet, "/api/v1/reach/providers/{providerId}/tests"),
		"/stewardmesh.v1.ReachService/ListTemplates":        endpoint(http.MethodGet, "/api/v1/reach/templates"),
		"/stewardmesh.v1.ReachService/CreateTemplate":       endpoint(http.MethodPost, "/api/v1/reach/templates"),
		"/stewardmesh.v1.ReachService/UpdateTemplate":       endpoint(http.MethodPut, "/api/v1/reach/templates/{templateId}"),
		"/stewardmesh.v1.ReachService/ListGroups":           endpoint(http.MethodGet, "/api/v1/reach/groups"),
		"/stewardmesh.v1.ReachService/CreateGroup":          endpoint(http.MethodPost, "/api/v1/reach/groups"),
		"/stewardmesh.v1.ReachService/UpdateGroup":          endpoint(http.MethodPut, "/api/v1/reach/groups/{groupId}"),
		"/stewardmesh.v1.ReachService/ListMessages":         endpoint(http.MethodGet, "/api/v1/reach/messages"),
		"/stewardmesh.v1.ReachService/SendMessage":          {method: http.MethodPost, path: "/api/v1/reach/messages/send", headerFields: map[string]string{"idempotencyKey": "Idempotency-Key"}},
		"/stewardmesh.v1.ReachService/RetryMessage":         endpoint(http.MethodPost, "/api/v1/reach/messages/{messageId}/retry"),
		"/stewardmesh.v1.ReachService/ListMessageAttempts":  endpoint(http.MethodGet, "/api/v1/reach/messages/{messageId}/attempts"),
		"/stewardmesh.v1.ReachService/ProcessSignals":       endpoint(http.MethodPost, "/api/v1/reach/signals/process"),
	}
	return result
}
