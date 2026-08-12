package httpapi

// Requirements: REQ-FOUNDATION-001, REQ-ATLAS-001, REQ-PEOPLE-001,
// REQ-DIRECTORY-EXPANSION-001, REQ-THREADS-001, REQ-STORAGE-001, REQ-LEDGER-001, REQ-HORIZON-001, REQ-PLATFORM-VALKEY-001,
// SEC-GUARD-001, SEC-HTTP-001.

import (
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/maxlemke/stewardmesh/internal/atlas"
	"github.com/maxlemke/stewardmesh/internal/bootstrap"
	"github.com/maxlemke/stewardmesh/internal/directoryexpansion"
	"github.com/maxlemke/stewardmesh/internal/foundation"
	"github.com/maxlemke/stewardmesh/internal/guard"
	"github.com/maxlemke/stewardmesh/internal/horizon"
	"github.com/maxlemke/stewardmesh/internal/identity"
	"github.com/maxlemke/stewardmesh/internal/ledger"
	"github.com/maxlemke/stewardmesh/internal/people"
	"github.com/maxlemke/stewardmesh/internal/storage"
	"github.com/maxlemke/stewardmesh/internal/threads"
)

const (
	csrfHeader                = "X-CSRF-Token"
	localSessionName          = "stewardmesh_session"
	secureSessionName         = "__Host-stewardmesh_session"
	localOIDCTransactionName  = "stewardmesh_oidc_transaction"
	secureOIDCTransactionName = "__Host-stewardmesh_oidc_transaction"
)

var correlationIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)

type Dependencies struct {
	Atlas               *atlas.Service
	People              *people.Service
	Threads             *threads.Service
	Vault               *storage.Service
	Ledger              *ledger.Service
	Horizon             *horizon.Service
	Guard               *guard.Service
	OIDC                *identity.OIDCFlow
	SAML                *identity.SAMLFlow
	Graph               directoryexpansion.GraphStore
	SessionCookieSecure bool
}

type Server struct {
	atlas               *atlas.Service
	people              *people.Service
	threads             *threads.Service
	vault               *storage.Service
	ledger              *ledger.Service
	horizon             *horizon.Service
	guard               *guard.Service
	oidc                *identity.OIDCFlow
	saml                *identity.SAMLFlow
	graph               directoryexpansion.GraphStore
	allowedOrigin       string
	organization        bootstrap.Organization
	sessionCookieSecure bool
}

type authenticatedHandler func(http.ResponseWriter, *http.Request, guard.Authentication)

type guardAccountResponse struct {
	ID          string `json:"id"`
	Username    string `json:"username"`
	Email       string `json:"email"`
	DisplayName string `json:"displayName"`
	Status      string `json:"status"`
}

type guardRoleResponse struct {
	ID              string             `json:"id"`
	Name            string             `json:"name"`
	Description     string             `json:"description"`
	Permissions     []guard.Permission `json:"permissions"`
	PolicyBundleIDs []string           `json:"policyBundleIds"`
	Source          string             `json:"source"`
	Managed         bool               `json:"managed"`
}

type guardPolicyBundleResponse struct {
	ID          string             `json:"id"`
	Name        string             `json:"name"`
	Description string             `json:"description"`
	Permissions []guard.Permission `json:"permissions"`
}

type guardScopeResponse struct {
	Kind       guard.ScopeKind `json:"kind"`
	ResourceID string          `json:"resourceId"`
}

type guardRoleAssignmentResponse struct {
	ID        string             `json:"id"`
	AccountID string             `json:"accountId"`
	RoleID    string             `json:"roleId"`
	Scope     guardScopeResponse `json:"scope"`
	Source    string             `json:"source"`
	Managed   bool               `json:"managed"`
	CreatedAt time.Time          `json:"createdAt"`
}

type guardResourceOwnershipResponse struct {
	ResourceType   string     `json:"resourceType"`
	ResourceID     string     `json:"resourceId"`
	SourceSystemID string     `json:"sourceSystemId"`
	SourceRecordID string     `json:"sourceRecordId"`
	WriteLocked    bool       `json:"writeLocked"`
	RegisteredAt   time.Time  `json:"registeredAt"`
	ClaimedBy      string     `json:"claimedBy,omitempty"`
	ClaimedAt      *time.Time `json:"claimedAt,omitempty"`
}

func NewServer(deps Dependencies, allowedOrigin string, organizations ...bootstrap.Organization) http.Handler {
	organization := bootstrap.Organization{ID: "local-organization", Name: "StewardMesh Local Organization"}
	if len(organizations) > 0 {
		organization = organizations[0]
	}
	server := &Server{
		atlas:               deps.Atlas,
		people:              deps.People,
		threads:             deps.Threads,
		vault:               deps.Vault,
		ledger:              deps.Ledger,
		horizon:             deps.Horizon,
		guard:               deps.Guard,
		oidc:                deps.OIDC,
		saml:                deps.SAML,
		graph:               deps.Graph,
		allowedOrigin:       allowedOrigin,
		organization:        organization,
		sessionCookieSecure: deps.SessionCookieSecure,
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", server.health)
	mux.HandleFunc("GET /api/v1/auth/bootstrap", server.bootstrapStatus)
	mux.HandleFunc("POST /api/v1/auth/bootstrap", server.bootstrapAdministrator)
	mux.HandleFunc("POST /api/v1/auth/login", server.login)
	mux.HandleFunc("GET /api/v1/auth/oidc/start", server.oidcStart)
	mux.HandleFunc("GET /api/v1/auth/oidc/callback", server.oidcCallback)
	mux.HandleFunc("GET /api/v1/auth/saml/metadata", server.samlMetadata)
	mux.HandleFunc("GET /api/v1/auth/saml/start", server.samlStart)
	mux.HandleFunc("POST /api/v1/auth/saml/acs", server.samlACS)
	mux.Handle("GET /api/v1/auth/session", server.protected("", false, server.getSession))
	mux.Handle("POST /api/v1/auth/logout", server.protected("", true, server.logout))
	mux.Handle("GET /api/v1/guard/access", server.protected(guard.PermissionGuardManage, false, server.listGuardAccess))
	mux.Handle("POST /api/v1/guard/roles", server.protected(guard.PermissionGuardManage, true, server.createGuardRole))
	mux.Handle("POST /api/v1/guard/role-assignments", server.protected(guard.PermissionGuardManage, true, server.createGuardRoleAssignment))
	mux.Handle("DELETE /api/v1/guard/role-assignments/{assignmentID}", server.protected(guard.PermissionGuardManage, true, server.deleteGuardRoleAssignment))
	mux.Handle("GET /api/v1/guard/resource-ownership", server.protected(guard.PermissionGuardManage, false, server.listGuardResourceOwnership))
	mux.Handle("POST /api/v1/guard/resource-ownership", server.protected(guard.PermissionGuardManage, true, server.registerGuardResourceOwnership))
	mux.Handle("POST /api/v1/guard/resource-ownership/{resourceType}/{resourceID}/claim", server.protected(guard.PermissionGuardManage, true, server.claimGuardResourceOwnership))
	mux.Handle("GET /api/v1/organization", server.protected(guard.PermissionOrganizationRead, false, server.getOrganization))
	mux.Handle("GET /api/v1/assets", server.protected(guard.PermissionAssetsRead, false, server.listAssets))
	mux.Handle("POST /api/v1/assets", server.protected(guard.PermissionAssetsWrite, true, server.createAsset))
	mux.Handle("GET /api/v1/assets/{assetID}", server.protected(guard.PermissionAssetsRead, false, server.getAsset))
	mux.Handle("PUT /api/v1/assets/{assetID}", server.protected(guard.PermissionAssetsWrite, true, server.updateAsset))
	mux.Handle("GET /api/v1/assets/{assetID}/lifecycle", server.protected(guard.PermissionAssetsRead, false, server.listAssetLifecycle))
	mux.Handle("GET /api/v1/sites", server.protected("", false, server.listSites))
	mux.Handle("POST /api/v1/sites", server.protected(guard.PermissionDirectoryWrite, true, server.createSite))
	mux.Handle("GET /api/v1/buildings", server.protected("", false, server.listBuildings))
	mux.Handle("POST /api/v1/buildings", server.protected(guard.PermissionDirectoryWrite, true, server.createBuilding))
	mux.Handle("GET /api/v1/rooms", server.protected("", false, server.listRooms))
	mux.Handle("POST /api/v1/rooms", server.protected(guard.PermissionDirectoryWrite, true, server.createRoom))
	mux.Handle("GET /api/v1/departments", server.protected("", false, server.listDepartments))
	mux.Handle("POST /api/v1/departments", server.protected(guard.PermissionDirectoryWrite, true, server.createDepartment))
	mux.Handle("GET /api/v1/identities", server.protected("", false, server.listIdentities))
	mux.Handle("POST /api/v1/identities", server.protected(guard.PermissionDirectoryWrite, true, server.createIdentity))
	mux.Handle("GET /api/v1/users", server.protected("", false, server.listUsers))
	mux.Handle("GET /api/v1/assets/{assetID}/assignments", server.protected(guard.PermissionAssetsRead, false, server.listAssetAssignments))
	mux.Handle("POST /api/v1/assets/{assetID}/assignments", server.protected(guard.PermissionDirectoryWrite, true, server.createAssetAssignment))
	mux.Handle("PATCH /api/v1/assets/{assetID}/assignments/{assignmentID}", server.protected(guard.PermissionDirectoryWrite, true, server.endAssetAssignment))
	mux.Handle("GET /api/v1/tags", server.protected(guard.PermissionGoalsRead, false, server.listTags))
	mux.Handle("POST /api/v1/tags", server.protected(guard.PermissionGoalsWrite, true, server.createTag))
	mux.Handle("GET /api/v1/tags/{tagID}", server.protected(guard.PermissionGoalsRead, false, server.getTag))
	mux.Handle("PUT /api/v1/tags/{tagID}", server.protected(guard.PermissionGoalsWrite, true, server.updateTag))
	mux.Handle("GET /api/v1/goals", server.protected(guard.PermissionGoalsRead, false, server.listGoals))
	mux.Handle("POST /api/v1/goals", server.protected(guard.PermissionGoalsWrite, true, server.createGoal))
	mux.Handle("GET /api/v1/goals/{goalID}", server.protected(guard.PermissionGoalsRead, false, server.getGoal))
	mux.Handle("PUT /api/v1/goals/{goalID}", server.protected(guard.PermissionGoalsWrite, true, server.updateGoal))
	mux.Handle("GET /api/v1/threads/{targetType}/{targetID}/tags", server.protected(guard.PermissionGoalsRead, false, server.listEffectiveTags))
	mux.Handle("PUT /api/v1/threads/{targetType}/{targetID}/tags/{tagID}", server.protected(guard.PermissionGoalsWrite, true, server.setTagRule))
	mux.Handle("DELETE /api/v1/threads/{targetType}/{targetID}/tags/{tagID}", server.protected(guard.PermissionGoalsWrite, true, server.deleteTagRule))
	mux.Handle("GET /api/v1/threads/{targetType}/{targetID}/goals", server.protected(guard.PermissionGoalsRead, false, server.listGoalLinks))
	mux.Handle("PUT /api/v1/threads/{targetType}/{targetID}/goals/{goalID}", server.protected(guard.PermissionGoalsWrite, true, server.linkGoal))
	mux.Handle("DELETE /api/v1/threads/{targetType}/{targetID}/goals/{goalID}", server.protected(guard.PermissionGoalsWrite, true, server.unlinkGoal))
	mux.Handle("GET /api/v1/blobs", server.protected(guard.PermissionStorageRead, false, server.listBlobs))
	mux.Handle("POST /api/v1/blobs", server.protected(guard.PermissionStorageWrite, true, server.createBlob))
	mux.Handle("GET /api/v1/blobs/{blobID}", server.protected(guard.PermissionStorageRead, false, server.getBlob))
	mux.Handle("GET /api/v1/blobs/{blobID}/content", server.protected(guard.PermissionStorageRead, false, server.downloadBlob))
	mux.Handle("POST /api/v1/blobs/{blobID}/download-authorization", server.protected(guard.PermissionStorageRead, true, server.authorizeBlobDownload))
	mux.Handle("GET /api/v1/ledger", server.protected(guard.PermissionFinanceRead, false, server.getLedgerSnapshot))
	mux.Handle("POST /api/v1/ledger/vendors", server.protected(guard.PermissionFinanceWrite, true, server.createLedgerVendor))
	mux.Handle("POST /api/v1/ledger/purchase-orders", server.protected(guard.PermissionFinanceWrite, true, server.createLedgerPurchaseOrder))
	mux.Handle("PUT /api/v1/ledger/purchase-orders/{purchaseOrderID}/status", server.protected(guard.PermissionFinanceWrite, true, server.updateLedgerPurchaseOrderStatus))
	mux.Handle("POST /api/v1/ledger/contracts", server.protected(guard.PermissionFinanceWrite, true, server.createLedgerContract))
	mux.Handle("PUT /api/v1/ledger/contracts/{contractID}/status", server.protected(guard.PermissionFinanceWrite, true, server.updateLedgerContractStatus))
	mux.Handle("POST /api/v1/ledger/commitments", server.protected(guard.PermissionFinanceWrite, true, server.createLedgerCommitment))
	mux.Handle("POST /api/v1/ledger/budgets", server.protected(guard.PermissionFinanceWrite, true, server.createLedgerBudget))
	mux.Handle("POST /api/v1/ledger/costs/reconcile", server.protected(guard.PermissionFinanceWrite, true, server.reconcileLedgerCost))
	mux.Handle("GET /api/v1/ledger/budget-variance", server.protected(guard.PermissionFinanceRead, false, server.getLedgerBudgetVariance))
	mux.Handle("GET /api/v1/ledger/export.csv", server.protected(guard.PermissionFinanceRead, false, server.exportLedgerCSV))
	mux.Handle("GET /api/v1/horizon/plans", server.protected(guard.PermissionPlanningRead, false, server.listHorizonPlans))
	mux.Handle("POST /api/v1/horizon/plans", server.protected(guard.PermissionPlanningWrite, true, server.createHorizonPlan))
	mux.Handle("PUT /api/v1/horizon/plans/{planID}", server.protected(guard.PermissionPlanningWrite, true, server.updateHorizonPlan))
	mux.Handle("GET /api/v1/horizon/plans/{planID}/history", server.protected(guard.PermissionPlanningRead, false, server.listHorizonPlanHistory))
	mux.Handle("GET /api/v1/horizon/forecast", server.protected(guard.PermissionPlanningRead, false, server.getHorizonForecast))
	mux.Handle("GET /api/v1/horizon/export.csv", server.protected(guard.PermissionPlanningRead, false, server.exportHorizonCSV))
	mux.Handle("GET /api/v1/graph", server.protected("", false, server.graphView))
	return server.correlation(server.securityHeaders(server.cors(mux)))
}

func (s *Server) getLedgerSnapshot(w http.ResponseWriter, r *http.Request, _ guard.Authentication) {
	if s.ledger == nil {
		writeError(w, r, http.StatusServiceUnavailable, "ledger_unavailable", "Ledger finance is unavailable")
		return
	}
	snapshot, err := s.ledger.Snapshot(r.Context())
	if err != nil {
		writeLedgerError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, snapshot)
}

func (s *Server) createLedgerVendor(w http.ResponseWriter, r *http.Request, _ guard.Authentication) {
	if s.ledger == nil {
		writeError(w, r, http.StatusServiceUnavailable, "ledger_unavailable", "Ledger finance is unavailable")
		return
	}
	var input ledger.CreateVendorInput
	if err := decodeJSON(w, r, 32<<10, &input); err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_request", "invalid vendor payload")
		return
	}
	created, err := s.ledger.CreateVendor(r.Context(), input)
	if err != nil {
		writeLedgerError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, created)
}

func (s *Server) createLedgerPurchaseOrder(w http.ResponseWriter, r *http.Request, _ guard.Authentication) {
	if s.ledger == nil {
		writeError(w, r, http.StatusServiceUnavailable, "ledger_unavailable", "Ledger finance is unavailable")
		return
	}
	var input ledger.CreatePurchaseOrderInput
	if err := decodeJSON(w, r, 128<<10, &input); err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_request", "invalid purchase order payload")
		return
	}
	created, err := s.ledger.CreatePurchaseOrder(r.Context(), input)
	if err != nil {
		writeLedgerError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, created)
}

func (s *Server) updateLedgerPurchaseOrderStatus(w http.ResponseWriter, r *http.Request, _ guard.Authentication) {
	if s.ledger == nil {
		writeError(w, r, http.StatusServiceUnavailable, "ledger_unavailable", "Ledger finance is unavailable")
		return
	}
	var input ledger.UpdatePurchaseOrderStatusInput
	if err := decodeJSON(w, r, 16<<10, &input); err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_request", "invalid purchase order status payload")
		return
	}
	input.ID = r.PathValue("purchaseOrderID")
	updated, err := s.ledger.UpdatePurchaseOrderStatus(r.Context(), input)
	if err != nil {
		writeLedgerError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

func (s *Server) createLedgerContract(w http.ResponseWriter, r *http.Request, _ guard.Authentication) {
	if s.ledger == nil {
		writeError(w, r, http.StatusServiceUnavailable, "ledger_unavailable", "Ledger finance is unavailable")
		return
	}
	var input ledger.CreateContractInput
	if err := decodeJSON(w, r, 128<<10, &input); err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_request", "invalid contract payload")
		return
	}
	created, err := s.ledger.CreateContract(r.Context(), input)
	if err != nil {
		writeLedgerError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, created)
}

func (s *Server) updateLedgerContractStatus(w http.ResponseWriter, r *http.Request, _ guard.Authentication) {
	if s.ledger == nil {
		writeError(w, r, http.StatusServiceUnavailable, "ledger_unavailable", "Ledger finance is unavailable")
		return
	}
	var input ledger.UpdateContractStatusInput
	if err := decodeJSON(w, r, 16<<10, &input); err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_request", "invalid contract status payload")
		return
	}
	input.ID = r.PathValue("contractID")
	updated, err := s.ledger.UpdateContractStatus(r.Context(), input)
	if err != nil {
		writeLedgerError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

func (s *Server) createLedgerCommitment(w http.ResponseWriter, r *http.Request, _ guard.Authentication) {
	if s.ledger == nil {
		writeError(w, r, http.StatusServiceUnavailable, "ledger_unavailable", "Ledger finance is unavailable")
		return
	}
	var input ledger.CreateCommitmentInput
	if err := decodeJSON(w, r, 64<<10, &input); err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_request", "invalid commitment payload")
		return
	}
	created, err := s.ledger.CreateCommitment(r.Context(), input)
	if err != nil {
		writeLedgerError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, created)
}

func (s *Server) createLedgerBudget(w http.ResponseWriter, r *http.Request, _ guard.Authentication) {
	if s.ledger == nil {
		writeError(w, r, http.StatusServiceUnavailable, "ledger_unavailable", "Ledger finance is unavailable")
		return
	}
	var input ledger.CreateBudgetInput
	if err := decodeJSON(w, r, 32<<10, &input); err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_request", "invalid budget payload")
		return
	}
	created, err := s.ledger.CreateBudget(r.Context(), input)
	if err != nil {
		writeLedgerError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, created)
}

func (s *Server) reconcileLedgerCost(w http.ResponseWriter, r *http.Request, _ guard.Authentication) {
	if s.ledger == nil {
		writeError(w, r, http.StatusServiceUnavailable, "ledger_unavailable", "Ledger finance is unavailable")
		return
	}
	var input ledger.ReconcileCostInput
	if err := decodeJSON(w, r, 64<<10, &input); err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_request", "invalid cost payload")
		return
	}
	result, err := s.ledger.ReconcileCost(r.Context(), input)
	if err != nil {
		writeLedgerError(w, r, err)
		return
	}
	status := http.StatusOK
	if result.Created {
		status = http.StatusCreated
	}
	writeJSON(w, status, result)
}

func (s *Server) getLedgerBudgetVariance(w http.ResponseWriter, r *http.Request, _ guard.Authentication) {
	if s.ledger == nil {
		writeError(w, r, http.StatusServiceUnavailable, "ledger_unavailable", "Ledger finance is unavailable")
		return
	}
	report, err := s.ledger.BudgetVariance(r.Context(), r.URL.Query().Get("fiscalPeriod"), r.URL.Query().Get("scenario"))
	if err != nil {
		writeLedgerError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, report)
}

func (s *Server) exportLedgerCSV(w http.ResponseWriter, r *http.Request, _ guard.Authentication) {
	if s.ledger == nil {
		writeError(w, r, http.StatusServiceUnavailable, "ledger_unavailable", "Ledger finance is unavailable")
		return
	}
	content, err := s.ledger.ExportCSV(r.Context(), r.URL.Query().Get("fiscalPeriod"), r.URL.Query().Get("scenario"))
	if err != nil {
		writeLedgerError(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="stewardmesh-ledger.csv"`)
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(content)
}

func writeLedgerError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, ledger.ErrInvalidInput):
		writeError(w, r, http.StatusBadRequest, "validation_failed", "Ledger details are invalid")
	case errors.Is(err, ledger.ErrReferenceMissing):
		writeError(w, r, http.StatusUnprocessableEntity, "reference_missing", "a referenced Ledger record is unavailable")
	case errors.Is(err, ledger.ErrNotFound):
		writeError(w, r, http.StatusNotFound, "not_found", "the requested Ledger record was not found")
	case errors.Is(err, ledger.ErrInvalidTransition):
		writeError(w, r, http.StatusConflict, "invalid_transition", "the requested Ledger status transition is not allowed")
	case errors.Is(err, ledger.ErrConflict):
		writeError(w, r, http.StatusConflict, "conflict", "this Ledger record conflicts with current data")
	default:
		writeError(w, r, http.StatusInternalServerError, "ledger_error", "the Ledger operation could not be completed")
	}
}

// Horizon handlers implement REQ-HORIZON-001 / lifecycle.planning.
func (s *Server) listHorizonPlans(w http.ResponseWriter, r *http.Request, _ guard.Authentication) {
	if s.horizon == nil {
		writeError(w, r, http.StatusServiceUnavailable, "horizon_unavailable", "Horizon planning is unavailable")
		return
	}
	plans, err := s.horizon.ListPlans(r.Context(), horizon.ListPlansQuery{
		AssetID: r.URL.Query().Get("assetId"), Scenario: r.URL.Query().Get("scenario"),
	})
	if err != nil {
		writeHorizonError(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, map[string]any{"items": plans})
}

func (s *Server) createHorizonPlan(w http.ResponseWriter, r *http.Request, _ guard.Authentication) {
	if s.horizon == nil {
		writeError(w, r, http.StatusServiceUnavailable, "horizon_unavailable", "Horizon planning is unavailable")
		return
	}
	var input horizon.CreatePlanInput
	if err := decodeJSON(w, r, 32<<10, &input); err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_request", "invalid Horizon plan payload")
		return
	}
	created, err := s.horizon.CreatePlan(r.Context(), input)
	if err != nil {
		writeHorizonError(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusCreated, created)
}

func (s *Server) updateHorizonPlan(w http.ResponseWriter, r *http.Request, _ guard.Authentication) {
	if s.horizon == nil {
		writeError(w, r, http.StatusServiceUnavailable, "horizon_unavailable", "Horizon planning is unavailable")
		return
	}
	var input horizon.UpdatePlanInput
	if err := decodeJSON(w, r, 32<<10, &input); err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_request", "invalid Horizon plan payload")
		return
	}
	input.ID = r.PathValue("planID")
	updated, err := s.horizon.UpdatePlan(r.Context(), input)
	if err != nil {
		writeHorizonError(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, updated)
}

func (s *Server) listHorizonPlanHistory(w http.ResponseWriter, r *http.Request, _ guard.Authentication) {
	if s.horizon == nil {
		writeError(w, r, http.StatusServiceUnavailable, "horizon_unavailable", "Horizon planning is unavailable")
		return
	}
	items, err := s.horizon.ListPlanHistory(r.Context(), r.PathValue("planID"))
	if err != nil {
		writeHorizonError(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) getHorizonForecast(w http.ResponseWriter, r *http.Request, _ guard.Authentication) {
	if s.horizon == nil {
		writeError(w, r, http.StatusServiceUnavailable, "horizon_unavailable", "Horizon planning is unavailable")
		return
	}
	query, err := horizonForecastQuery(r)
	if err != nil {
		writeHorizonError(w, r, err)
		return
	}
	report, err := s.horizon.Forecast(r.Context(), query)
	if err != nil {
		writeHorizonError(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, report)
}

func (s *Server) exportHorizonCSV(w http.ResponseWriter, r *http.Request, _ guard.Authentication) {
	if s.horizon == nil {
		writeError(w, r, http.StatusServiceUnavailable, "horizon_unavailable", "Horizon planning is unavailable")
		return
	}
	query, err := horizonForecastQuery(r)
	if err != nil {
		writeHorizonError(w, r, err)
		return
	}
	content, err := s.horizon.ExportCSV(r.Context(), query)
	if err != nil {
		writeHorizonError(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="stewardmesh-horizon-forecast.csv"`)
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(content)
}

func horizonForecastQuery(r *http.Request) (horizon.ForecastQuery, error) {
	values := r.URL.Query()
	query := horizon.ForecastQuery{GroupBy: values.Get("groupBy")}
	if raw := strings.TrimSpace(values.Get("scenarios")); raw != "" {
		query.Scenarios = strings.Split(raw, ",")
	}
	if raw := strings.TrimSpace(values.Get("asOf")); raw != "" {
		parsed, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			return horizon.ForecastQuery{}, horizon.ErrInvalidInput
		}
		query.AsOf = parsed
	}
	var err error
	if query.FromYear, err = optionalQueryInt(values.Get("fromYear")); err != nil {
		return horizon.ForecastQuery{}, horizon.ErrInvalidInput
	}
	if query.ToYear, err = optionalQueryInt(values.Get("toYear")); err != nil {
		return horizon.ForecastQuery{}, horizon.ErrInvalidInput
	}
	if query.FiscalYearStartMonth, err = optionalQueryInt(values.Get("fiscalYearStartMonth")); err != nil {
		return horizon.ForecastQuery{}, horizon.ErrInvalidInput
	}
	return query, nil
}

func optionalQueryInt(value string) (int, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, nil
	}
	return strconv.Atoi(value)
}

func writeHorizonError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, horizon.ErrInvalidInput):
		writeError(w, r, http.StatusBadRequest, "validation_failed", "Horizon planning details are invalid")
	case errors.Is(err, horizon.ErrReferenceMissing):
		writeError(w, r, http.StatusUnprocessableEntity, "reference_missing", "a referenced Horizon record is unavailable")
	case errors.Is(err, horizon.ErrNotFound):
		writeError(w, r, http.StatusNotFound, "not_found", "the requested Horizon plan was not found")
	case errors.Is(err, horizon.ErrMixedCurrency):
		writeError(w, r, http.StatusConflict, "mixed_currency", "Horizon cannot combine values with different currencies")
	case errors.Is(err, horizon.ErrConflict):
		writeError(w, r, http.StatusConflict, "conflict", "this Horizon plan conflicts with current data")
	default:
		writeError(w, r, http.StatusInternalServerError, "horizon_error", "the Horizon operation could not be completed")
	}
}

func (s *Server) listBlobs(w http.ResponseWriter, r *http.Request, _ guard.Authentication) {
	if s.vault == nil {
		writeError(w, r, http.StatusServiceUnavailable, "vault_unavailable", "Vault storage is unavailable")
		return
	}
	blobs, err := s.vault.ListBlobs(r.Context())
	if err != nil {
		writeVaultError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": blobs, "maximumUploadBytes": s.vault.MaximumUploadBytes()})
}

func (s *Server) getBlob(w http.ResponseWriter, r *http.Request, _ guard.Authentication) {
	if s.vault == nil {
		writeError(w, r, http.StatusServiceUnavailable, "vault_unavailable", "Vault storage is unavailable")
		return
	}
	blob, err := s.vault.GetBlob(r.Context(), r.PathValue("blobID"))
	if err != nil {
		writeVaultError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, blob)
}

func (s *Server) createBlob(w http.ResponseWriter, r *http.Request, _ guard.Authentication) {
	if s.vault == nil {
		writeError(w, r, http.StatusServiceUnavailable, "vault_unavailable", "Vault storage is unavailable")
		return
	}
	maximumBodyBytes := s.vault.MaximumUploadBytes() + 1<<20
	r.Body = http.MaxBytesReader(w, r.Body, maximumBodyBytes)
	contentType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || contentType != "multipart/form-data" {
		writeError(w, r, http.StatusUnsupportedMediaType, "content_type_invalid", "Vault uploads require multipart form data")
		return
	}
	if err := r.ParseMultipartForm(1 << 20); err != nil {
		var maximumError *http.MaxBytesError
		if errors.As(err, &maximumError) {
			writeError(w, r, http.StatusRequestEntityTooLarge, "blob_too_large", "the file exceeds the configured Vault size limit")
			return
		}
		writeError(w, r, http.StatusBadRequest, "validation_failed", "the Vault upload form is invalid")
		return
	}
	if r.MultipartForm != nil {
		defer r.MultipartForm.RemoveAll()
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, r, http.StatusBadRequest, "validation_failed", "a file is required")
		return
	}
	defer file.Close()
	if header.Size > s.vault.MaximumUploadBytes() {
		writeError(w, r, http.StatusRequestEntityTooLarge, "blob_too_large", "the file exceeds the configured Vault size limit")
		return
	}
	mediaType, _, err := mime.ParseMediaType(header.Header.Get("Content-Type"))
	if err != nil || mediaType == "" {
		mediaType = "application/octet-stream"
	}
	blob, err := s.vault.CreateBlob(r.Context(), storage.CreateBlobInput{
		Name: header.Filename, MediaType: mediaType, Content: file,
		SourceSystemID: r.FormValue("sourceSystemId"), SourceRecordID: r.FormValue("sourceRecordId"),
		ResourceType: r.FormValue("resourceType"), ResourceID: r.FormValue("resourceId"),
	})
	if err != nil {
		writeVaultError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, blob)
}

func (s *Server) downloadBlob(w http.ResponseWriter, r *http.Request, _ guard.Authentication) {
	if s.vault == nil {
		writeError(w, r, http.StatusServiceUnavailable, "vault_unavailable", "Vault storage is unavailable")
		return
	}
	blob, content, err := s.vault.OpenAuthorizedBlob(r.Context(), r.PathValue("blobID"), r.URL.Query().Get("token"))
	if err != nil {
		writeVaultError(w, r, err)
		return
	}
	defer content.Close()
	w.Header().Set("Content-Type", blob.MediaType)
	w.Header().Set("Content-Length", strconv.FormatInt(blob.SizeBytes, 10))
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": blob.Name}))
	w.Header().Set("ETag", `"sha256:`+blob.SHA256+`"`)
	w.WriteHeader(http.StatusOK)
	_, _ = io.Copy(w, content)
}

func (s *Server) authorizeBlobDownload(w http.ResponseWriter, r *http.Request, _ guard.Authentication) {
	if s.vault == nil {
		writeError(w, r, http.StatusServiceUnavailable, "vault_unavailable", "Vault storage is unavailable")
		return
	}
	authorization, err := s.vault.AuthorizeDownload(r.Context(), r.PathValue("blobID"))
	if err != nil {
		writeVaultError(w, r, err)
		return
	}
	if strings.HasPrefix(authorization.URL, "?token=") {
		authorization.URL = "/api/v1/blobs/" + r.PathValue("blobID") + "/content" + authorization.URL
	}
	writeJSON(w, http.StatusCreated, authorization)
}

func writeVaultError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, storage.ErrInvalidInput):
		writeError(w, r, http.StatusBadRequest, "validation_failed", "Vault file details are invalid")
	case errors.Is(err, storage.ErrNotFound):
		writeError(w, r, http.StatusNotFound, "not_found", "the requested Vault file was not found")
	case errors.Is(err, storage.ErrConflict):
		writeError(w, r, http.StatusConflict, "conflict", "this Vault file conflicts with existing data")
	case errors.Is(err, storage.ErrTooLarge):
		writeError(w, r, http.StatusRequestEntityTooLarge, "blob_too_large", "the file exceeds the configured Vault size limit")
	case errors.Is(err, storage.ErrIntegrity):
		writeError(w, r, http.StatusInternalServerError, "integrity_failed", "Vault could not verify the stored file")
	default:
		writeError(w, r, http.StatusInternalServerError, "vault_error", "the Vault operation could not be completed")
	}
}

func (s *Server) graphView(w http.ResponseWriter, r *http.Request, authentication guard.Authentication) {
	if s.graph == nil {
		writeError(w, r, http.StatusServiceUnavailable, "graph_unavailable", "relationship graph unavailable")
		return
	}
	if _, ok := s.directoryVisibility(w, r, authentication); !ok {
		return
	}
	query := directoryexpansion.GraphQuery{
		Search:       r.URL.Query().Get("search"),
		Kind:         r.URL.Query().Get("kind"),
		Relationship: r.URL.Query().Get("relationship"),
	}
	if value := r.URL.Query().Get("limit"); value != "" {
		if parsed, err := strconv.Atoi(value); err == nil {
			query.Limit = parsed
		}
	}
	graph, err := s.graph.Graph(r.Context(), query)
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "graph_error", "unable to load relationship graph")
		return
	}
	writeJSON(w, http.StatusOK, graph)
}

func (s *Server) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "product": "StewardMesh", "organizationId": s.organization.ID})
}

func (s *Server) bootstrapStatus(w http.ResponseWriter, r *http.Request) {
	s.noStore(w)
	if s.guard == nil {
		writeError(w, r, http.StatusServiceUnavailable, "authentication_unavailable", "authentication service unavailable")
		return
	}
	required, tokenRequired, err := s.guard.BootstrapStatus(r.Context())
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "authentication_error", "unable to read administrator setup status")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"required":                  required,
		"tokenRequired":             tokenRequired,
		"minimumPasswordCharacters": guard.MinimumPasswordCharacters,
		"oidcEnabled":               s.oidc != nil,
		"samlEnabled":               s.saml != nil,
	})
}

func (s *Server) bootstrapAdministrator(w http.ResponseWriter, r *http.Request) {
	s.noStore(w)
	if s.guard == nil {
		writeError(w, r, http.StatusServiceUnavailable, "authentication_unavailable", "authentication service unavailable")
		return
	}
	var input struct {
		Username       string `json:"username"`
		Email          string `json:"email"`
		DisplayName    string `json:"displayName"`
		Password       string `json:"password"`
		BootstrapToken string `json:"bootstrapToken"`
	}
	if err := decodeJSON(w, r, 32<<10, &input); err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_request", "invalid administrator setup payload")
		return
	}
	credentials, err := s.guard.Bootstrap(r.Context(), guard.BootstrapInput{
		Username:       input.Username,
		Email:          input.Email,
		DisplayName:    input.DisplayName,
		Password:       input.Password,
		BootstrapToken: input.BootstrapToken,
	}, s.trustedBrowserRequest(r))
	switch {
	case errors.Is(err, guard.ErrBootstrapDenied):
		writeError(w, r, http.StatusForbidden, "bootstrap_denied", "administrator setup is not authorized")
		return
	case errors.Is(err, guard.ErrBootstrapComplete):
		writeError(w, r, http.StatusConflict, "bootstrap_complete", "administrator setup is already complete")
		return
	case errors.Is(err, guard.ErrInvalidInput):
		writeError(w, r, http.StatusBadRequest, "validation_failed", "administrator details do not meet setup requirements")
		return
	case err != nil:
		writeError(w, r, http.StatusInternalServerError, "authentication_error", "unable to create the administrator")
		return
	}
	s.writeAuthenticatedSession(w, http.StatusCreated, credentials)
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	s.noStore(w)
	if s.guard == nil {
		writeError(w, r, http.StatusServiceUnavailable, "authentication_unavailable", "authentication service unavailable")
		return
	}
	if !s.trustedBrowserRequest(r) {
		writeError(w, r, http.StatusForbidden, "origin_denied", "request origin is not allowed")
		return
	}
	var input struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := decodeJSON(w, r, 16<<10, &input); err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_request", "invalid login payload")
		return
	}
	credentials, err := s.guard.Login(r.Context(), guard.LoginInput{
		Username: input.Username,
		Password: input.Password,
		RateKey:  remoteHost(r.RemoteAddr),
	})
	switch {
	case errors.Is(err, guard.ErrLoginProtectionUnavailable):
		writeError(w, r, http.StatusServiceUnavailable, "authentication_unavailable", "login protection is temporarily unavailable")
		return
	case errors.Is(err, guard.ErrRateLimited):
		w.Header().Set("Retry-After", "900")
		writeError(w, r, http.StatusTooManyRequests, "rate_limited", "too many login attempts; try again later")
		return
	case errors.Is(err, guard.ErrInvalidCredential):
		writeError(w, r, http.StatusUnauthorized, "invalid_credentials", "invalid username or password")
		return
	case err != nil:
		writeError(w, r, http.StatusInternalServerError, "authentication_error", "unable to sign in")
		return
	}
	s.writeAuthenticatedSession(w, http.StatusOK, credentials)
}

func (s *Server) oidcStart(w http.ResponseWriter, r *http.Request) {
	s.noStore(w)
	if s.guard == nil || s.oidc == nil {
		writeError(w, r, http.StatusServiceUnavailable, "oidc_unavailable", "OpenID Connect sign-in is unavailable")
		return
	}
	if !s.trustedExternalAuthStart(r) {
		writeError(w, r, http.StatusForbidden, "origin_denied", "request origin is not allowed")
		return
	}
	required, _, err := s.guard.BootstrapStatus(r.Context())
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "authentication_error", "unable to read administrator setup status")
		return
	}
	if required {
		writeError(w, r, http.StatusConflict, "bootstrap_required", "create the first administrator before using OpenID Connect")
		return
	}
	authorizationURL, transactionValue, expiresAt, err := s.oidc.Start()
	if err != nil {
		s.guard.RecordOIDCFailure(r.Context())
		writeError(w, r, http.StatusServiceUnavailable, "oidc_unavailable", "OpenID Connect sign-in is unavailable")
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     s.oidcTransactionCookieName(),
		Value:    transactionValue,
		Path:     "/",
		Expires:  expiresAt,
		MaxAge:   max(1, int(time.Until(expiresAt).Seconds())),
		HttpOnly: true,
		Secure:   s.sessionCookieSecure,
		SameSite: http.SameSiteLaxMode,
	})
	http.Redirect(w, r, authorizationURL, http.StatusSeeOther)
}

func (s *Server) oidcCallback(w http.ResponseWriter, r *http.Request) {
	s.noStore(w)
	if s.guard == nil || s.oidc == nil {
		writeError(w, r, http.StatusServiceUnavailable, "oidc_unavailable", "OpenID Connect sign-in is unavailable")
		return
	}
	cookie, cookieErr := r.Cookie(s.oidcTransactionCookieName())
	s.clearOIDCTransactionCookie(w)
	state := r.URL.Query().Get("state")
	if cookieErr != nil || cookie.Value == "" {
		s.oidcFailure(w, r)
		return
	}
	if r.URL.Query().Get("error") != "" {
		if err := s.oidc.Validate(cookie.Value, state); err != nil {
			s.oidcFailure(w, r)
			return
		}
		s.oidcFailure(w, r)
		return
	}
	principal, err := s.oidc.Complete(r.Context(), cookie.Value, state, r.URL.Query().Get("code"))
	if err != nil {
		s.oidcFailure(w, r)
		return
	}
	credentials, err := s.guard.LoginOIDC(r.Context(), principal)
	if err != nil {
		s.oidcFailureRedirect(w, r)
		return
	}
	s.setSessionCookie(w, credentials)
	http.Redirect(w, r, s.allowedOrigin, http.StatusSeeOther)
}

func (s *Server) oidcFailure(w http.ResponseWriter, r *http.Request) {
	s.guard.RecordOIDCFailure(r.Context())
	s.oidcFailureRedirect(w, r)
}

func (s *Server) oidcFailureRedirect(w http.ResponseWriter, r *http.Request) {
	if s.allowedOrigin == "" {
		writeError(w, r, http.StatusUnauthorized, "oidc_failed", "OpenID Connect sign-in could not be completed")
		return
	}
	http.Redirect(w, r, s.allowedOrigin+"?auth=oidc_error", http.StatusSeeOther)
}

func (s *Server) samlMetadata(w http.ResponseWriter, r *http.Request) {
	s.noStore(w)
	if s.saml == nil {
		writeError(w, r, http.StatusNotFound, "saml_unavailable", "SAML sign-in is unavailable")
		return
	}
	metadata, err := s.saml.Metadata()
	if err != nil {
		writeError(w, r, http.StatusServiceUnavailable, "saml_unavailable", "SAML metadata is unavailable")
		return
	}
	w.Header().Set("Content-Type", "application/samlmetadata+xml; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(metadata)
}

func (s *Server) samlStart(w http.ResponseWriter, r *http.Request) {
	s.noStore(w)
	if s.guard == nil || s.saml == nil {
		writeError(w, r, http.StatusServiceUnavailable, "saml_unavailable", "SAML sign-in is unavailable")
		return
	}
	if !s.trustedExternalAuthStart(r) {
		writeError(w, r, http.StatusForbidden, "origin_denied", "request origin is not allowed")
		return
	}
	required, _, err := s.guard.BootstrapStatus(r.Context())
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "authentication_error", "unable to read administrator setup status")
		return
	}
	if required {
		writeError(w, r, http.StatusConflict, "bootstrap_required", "create the first administrator before using SAML")
		return
	}
	authorizationURL, relayState, requestID, expiresAt, err := s.saml.Start()
	if err != nil || s.guard.TrackSAMLRequest(r.Context(), relayState, requestID, expiresAt) != nil {
		s.guard.RecordSAMLFailure(r.Context())
		writeError(w, r, http.StatusServiceUnavailable, "saml_unavailable", "SAML sign-in is unavailable")
		return
	}
	http.Redirect(w, r, authorizationURL, http.StatusSeeOther)
}

func (s *Server) samlACS(w http.ResponseWriter, r *http.Request) {
	s.noStore(w)
	if s.guard == nil || s.saml == nil {
		writeError(w, r, http.StatusServiceUnavailable, "saml_unavailable", "SAML sign-in is unavailable")
		return
	}
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/x-www-form-urlencoded" {
		s.samlFailure(w, r)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 2<<20)
	if err := r.ParseForm(); err != nil || !validSAMLResponseForm(r.Form) {
		s.samlFailure(w, r)
		return
	}
	expectedRequestID, err := s.guard.ConsumeSAMLRequest(r.Context(), r.Form.Get("RelayState"))
	if err != nil {
		s.samlFailure(w, r)
		return
	}
	principal, err := s.saml.Complete(r, expectedRequestID)
	if err != nil {
		s.samlFailure(w, r)
		return
	}
	credentials, err := s.guard.LoginSAML(r.Context(), principal)
	if err != nil {
		s.samlFailureRedirect(w, r)
		return
	}
	s.setSessionCookie(w, credentials)
	http.Redirect(w, r, s.allowedOrigin, http.StatusSeeOther)
}

func validSAMLResponseForm(form url.Values) bool {
	if len(form) != 2 || form.Get("RelayState") == "" || len(form.Get("RelayState")) > 80 || form.Get("SAMLResponse") == "" {
		return false
	}
	for name, values := range form {
		if name != "RelayState" && name != "SAMLResponse" || len(values) != 1 {
			return false
		}
	}
	return true
}

func (s *Server) samlFailure(w http.ResponseWriter, r *http.Request) {
	s.guard.RecordSAMLFailure(r.Context())
	s.samlFailureRedirect(w, r)
}

func (s *Server) samlFailureRedirect(w http.ResponseWriter, r *http.Request) {
	if s.allowedOrigin == "" {
		writeError(w, r, http.StatusUnauthorized, "saml_failed", "SAML sign-in could not be completed")
		return
	}
	http.Redirect(w, r, s.allowedOrigin+"?auth=saml_error", http.StatusSeeOther)
}

func (s *Server) getSession(w http.ResponseWriter, r *http.Request, authentication guard.Authentication) {
	s.noStore(w)
	csrfToken, err := s.guard.RefreshCSRF(r.Context(), &authentication)
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "authentication_error", "unable to refresh the session")
		return
	}
	writeJSON(w, http.StatusOK, sessionResponse(authentication, csrfToken))
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request, authentication guard.Authentication) {
	s.noStore(w)
	if err := s.guard.Logout(r.Context(), authentication); err != nil {
		writeError(w, r, http.StatusInternalServerError, "authentication_error", "unable to sign out")
		return
	}
	s.clearSessionCookie(w)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) listGuardAccess(w http.ResponseWriter, r *http.Request, authentication guard.Authentication) {
	s.noStore(w)
	directory, err := s.guard.ListAuthorization(r.Context(), authentication)
	if err != nil {
		s.writeGuardManagementError(w, r, err)
		return
	}
	accounts := make([]guardAccountResponse, 0, len(directory.Accounts))
	for _, account := range directory.Accounts {
		accounts = append(accounts, guardAccountResponse{
			ID: account.ID, Username: account.Username, Email: account.Email,
			DisplayName: account.DisplayName, Status: account.Status,
		})
	}
	roles := make([]guardRoleResponse, 0, len(directory.Roles))
	for _, role := range directory.Roles {
		roles = append(roles, guardRoleResponse{
			ID: role.ID, Name: role.Name, Description: role.Description,
			Permissions:     append([]guard.Permission(nil), role.Permissions...),
			PolicyBundleIDs: append([]string(nil), role.PolicyBundleIDs...),
			Source:          role.Source, Managed: role.Source == guard.BuiltInRoleSource,
		})
	}
	policyBundles := make([]guardPolicyBundleResponse, 0, len(directory.PolicyBundles))
	for _, bundle := range directory.PolicyBundles {
		policyBundles = append(policyBundles, guardPolicyBundleResponse{
			ID: bundle.ID, Name: bundle.Name, Description: bundle.Description,
			Permissions: append([]guard.Permission(nil), bundle.Permissions...),
		})
	}
	assignments := make([]guardRoleAssignmentResponse, 0, len(directory.Assignments))
	for _, assignment := range directory.Assignments {
		assignments = append(assignments, guardAssignmentResponse(assignment))
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"accounts": accounts, "roles": roles, "policyBundles": policyBundles,
		"availablePermissions": append([]guard.Permission(nil), directory.AvailablePermissions...),
		"assignments":          assignments,
	})
}

func (s *Server) createGuardRole(w http.ResponseWriter, r *http.Request, authentication guard.Authentication) {
	s.noStore(w)
	var input struct {
		Name            string             `json:"name"`
		Description     string             `json:"description"`
		Permissions     []guard.Permission `json:"permissions"`
		PolicyBundleIDs []string           `json:"policyBundleIds"`
	}
	if err := decodeJSON(w, r, 32<<10, &input); err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_request", "invalid role payload")
		return
	}
	role, err := s.guard.CreateRole(r.Context(), authentication, guard.CreateRoleInput{
		Name: input.Name, Description: input.Description,
		Permissions: input.Permissions, PolicyBundleIDs: input.PolicyBundleIDs,
	})
	if err != nil {
		s.writeGuardManagementError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, guardRoleResponse{
		ID: role.ID, Name: role.Name, Description: role.Description,
		Permissions:     append([]guard.Permission(nil), role.Permissions...),
		PolicyBundleIDs: append([]string(nil), role.PolicyBundleIDs...),
		Source:          role.Source, Managed: false,
	})
}

func (s *Server) createGuardRoleAssignment(w http.ResponseWriter, r *http.Request, authentication guard.Authentication) {
	s.noStore(w)
	var input struct {
		AccountID string `json:"accountId"`
		RoleID    string `json:"roleId"`
		Scope     struct {
			Kind       string `json:"kind"`
			ResourceID string `json:"resourceId"`
		} `json:"scope"`
	}
	if err := decodeJSON(w, r, 16<<10, &input); err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_request", "invalid role assignment payload")
		return
	}
	assignment, err := s.guard.AssignRole(r.Context(), authentication, guard.RoleAssignmentInput{
		AccountID: input.AccountID, RoleID: input.RoleID,
		ScopeKind: guard.ScopeKind(input.Scope.Kind), ResourceID: input.Scope.ResourceID,
	})
	if err != nil {
		s.writeGuardManagementError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, guardAssignmentResponse(assignment))
}

func (s *Server) deleteGuardRoleAssignment(w http.ResponseWriter, r *http.Request, authentication guard.Authentication) {
	s.noStore(w)
	if _, err := s.guard.RevokeRoleAssignment(r.Context(), authentication, r.PathValue("assignmentID")); err != nil {
		s.writeGuardManagementError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) listGuardResourceOwnership(w http.ResponseWriter, r *http.Request, authentication guard.Authentication) {
	s.noStore(w)
	ownership, err := s.guard.ListResourceOwnership(r.Context(), authentication)
	if err != nil {
		s.writeGuardManagementError(w, r, err)
		return
	}
	items := make([]guardResourceOwnershipResponse, 0, len(ownership))
	for _, record := range ownership {
		items = append(items, guardOwnershipResponse(record))
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) registerGuardResourceOwnership(w http.ResponseWriter, r *http.Request, authentication guard.Authentication) {
	s.noStore(w)
	var input struct {
		ResourceType   string `json:"resourceType"`
		ResourceID     string `json:"resourceId"`
		SourceSystemID string `json:"sourceSystemId"`
		SourceRecordID string `json:"sourceRecordId"`
	}
	if err := decodeJSON(w, r, 16<<10, &input); err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_request", "invalid resource ownership payload")
		return
	}
	ownership, created, err := s.guard.RegisterResourceOwnership(r.Context(), authentication, guard.ResourceOwnershipInput{
		ResourceType: input.ResourceType, ResourceID: input.ResourceID,
		SourceSystemID: input.SourceSystemID, SourceRecordID: input.SourceRecordID,
	})
	if err != nil {
		s.writeGuardManagementError(w, r, err)
		return
	}
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	writeJSON(w, status, guardOwnershipResponse(ownership))
}

func (s *Server) claimGuardResourceOwnership(w http.ResponseWriter, r *http.Request, authentication guard.Authentication) {
	s.noStore(w)
	ownership, err := s.guard.ClaimResourceOwnership(
		r.Context(), authentication, r.PathValue("resourceType"), r.PathValue("resourceID"),
	)
	if err != nil {
		s.writeGuardManagementError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, guardOwnershipResponse(ownership))
}

func guardAssignmentResponse(assignment guard.RoleAssignment) guardRoleAssignmentResponse {
	return guardRoleAssignmentResponse{
		ID: assignment.ID, AccountID: assignment.AccountID, RoleID: assignment.RoleID,
		Scope:  guardScopeResponse{Kind: assignment.Scope.Kind, ResourceID: assignment.Scope.ResourceID},
		Source: assignment.Source, Managed: assignment.Source != guard.LocalAssignmentSource, CreatedAt: assignment.CreatedAt,
	}
}

func guardOwnershipResponse(ownership guard.ResourceOwnership) guardResourceOwnershipResponse {
	return guardResourceOwnershipResponse{
		ResourceType: ownership.ResourceType, ResourceID: ownership.ResourceID,
		SourceSystemID: ownership.SourceSystemID, SourceRecordID: ownership.SourceRecordID,
		WriteLocked: ownership.WriteLocked, RegisteredAt: ownership.RegisteredAt,
		ClaimedBy: ownership.ClaimedBy, ClaimedAt: ownership.ClaimedAt,
	}
}

func (s *Server) writeGuardManagementError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, guard.ErrInvalidInput):
		writeError(w, r, http.StatusBadRequest, "validation_failed", "Guard management details are invalid")
	case errors.Is(err, guard.ErrNotFound):
		writeError(w, r, http.StatusNotFound, "not_found", "the requested Guard record was not found")
	case errors.Is(err, guard.ErrManagedAssignment):
		writeError(w, r, http.StatusConflict, "managed_assignment", "this role assignment is managed by the identity provider")
	case errors.Is(err, guard.ErrLastAdministrator):
		writeError(w, r, http.StatusConflict, "last_administrator", "Assign another organization administrator before removing this assignment")
	case errors.Is(err, guard.ErrBuiltInRole):
		writeError(w, r, http.StatusConflict, "built_in_role", "built-in roles cannot be changed")
	case errors.Is(err, guard.ErrConflict):
		writeError(w, r, http.StatusConflict, "conflict", "this Guard record conflicts with existing data")
	case errors.Is(err, guard.ErrPermissionDenied):
		writeError(w, r, http.StatusForbidden, "permission_denied", "organization-level Guard management permission is required")
	default:
		writeError(w, r, http.StatusInternalServerError, "guard_error", "the Guard management operation could not be completed")
	}
}

func (s *Server) getOrganization(w http.ResponseWriter, _ *http.Request, _ guard.Authentication) {
	writeJSON(w, http.StatusOK, s.organization)
}

func (s *Server) listAssets(w http.ResponseWriter, r *http.Request, _ guard.Authentication) {
	if s.atlas == nil {
		writeError(w, r, http.StatusServiceUnavailable, "repository_unavailable", "asset repository unavailable")
		return
	}
	query, err := assetQueryFromRequest(r)
	if err != nil {
		writeError(w, r, http.StatusBadRequest, "validation_failed", "asset filters are invalid")
		return
	}
	assets, err := s.atlas.ListAssets(r.Context(), query)
	if err != nil {
		writeAtlasError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": assets})
}

func (s *Server) getAsset(w http.ResponseWriter, r *http.Request, _ guard.Authentication) {
	if s.atlas == nil {
		writeError(w, r, http.StatusServiceUnavailable, "repository_unavailable", "asset repository unavailable")
		return
	}
	asset, err := s.atlas.GetAsset(r.Context(), r.PathValue("assetID"))
	if err != nil {
		writeAtlasError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, asset)
}

func (s *Server) listSites(w http.ResponseWriter, r *http.Request, authentication guard.Authentication) {
	if s.people == nil {
		writeError(w, r, http.StatusServiceUnavailable, "repository_unavailable", "people directory unavailable")
		return
	}
	visibility, ok := s.directoryVisibility(w, r, authentication)
	if !ok {
		return
	}
	items, err := s.people.ListSites(r.Context(), visibility)
	if err != nil {
		writePeopleError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) createSite(w http.ResponseWriter, r *http.Request, _ guard.Authentication) {
	if s.people == nil {
		writeError(w, r, http.StatusServiceUnavailable, "repository_unavailable", "people directory unavailable")
		return
	}
	var input struct {
		Name    string              `json:"name"`
		Address people.Address      `json:"address"`
		Status  people.RecordStatus `json:"status"`
	}
	if err := decodeJSON(w, r, 32<<10, &input); err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_request", "invalid site payload")
		return
	}
	created, err := s.people.CreateSite(r.Context(), people.CreateSiteInput{
		Name: input.Name, Address: input.Address, Status: input.Status,
	})
	if err != nil {
		writePeopleError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, created)
}

func (s *Server) listBuildings(w http.ResponseWriter, r *http.Request, authentication guard.Authentication) {
	if s.people == nil {
		writeError(w, r, http.StatusServiceUnavailable, "repository_unavailable", "people directory unavailable")
		return
	}
	visibility, ok := s.directoryVisibility(w, r, authentication)
	if !ok {
		return
	}
	items, err := s.people.ListBuildings(r.Context(), r.URL.Query().Get("siteId"), visibility)
	if err != nil {
		writePeopleError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) createBuilding(w http.ResponseWriter, r *http.Request, _ guard.Authentication) {
	if s.people == nil {
		writeError(w, r, http.StatusServiceUnavailable, "repository_unavailable", "people directory unavailable")
		return
	}
	var input struct {
		SiteID string              `json:"siteId"`
		Name   string              `json:"name"`
		Status people.RecordStatus `json:"status"`
	}
	if err := decodeJSON(w, r, 32<<10, &input); err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_request", "invalid building payload")
		return
	}
	created, err := s.people.CreateBuilding(r.Context(), people.CreateBuildingInput{
		SiteID: input.SiteID, Name: input.Name, Status: input.Status,
	})
	if err != nil {
		writePeopleError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, created)
}

func (s *Server) listRooms(w http.ResponseWriter, r *http.Request, authentication guard.Authentication) {
	if s.people == nil {
		writeError(w, r, http.StatusServiceUnavailable, "repository_unavailable", "people directory unavailable")
		return
	}
	visibility, ok := s.directoryVisibility(w, r, authentication)
	if !ok {
		return
	}
	items, err := s.people.ListRooms(
		r.Context(),
		r.URL.Query().Get("siteId"),
		r.URL.Query().Get("buildingId"),
		visibility,
	)
	if err != nil {
		writePeopleError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) createRoom(w http.ResponseWriter, r *http.Request, _ guard.Authentication) {
	if s.people == nil {
		writeError(w, r, http.StatusServiceUnavailable, "repository_unavailable", "people directory unavailable")
		return
	}
	var input struct {
		SiteID     string              `json:"siteId"`
		BuildingID string              `json:"buildingId"`
		Number     string              `json:"number"`
		Name       string              `json:"name"`
		Status     people.RecordStatus `json:"status"`
	}
	if err := decodeJSON(w, r, 32<<10, &input); err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_request", "invalid room payload")
		return
	}
	created, err := s.people.CreateRoom(r.Context(), people.CreateRoomInput{
		SiteID: input.SiteID, BuildingID: input.BuildingID, Number: input.Number,
		Name: input.Name, Status: input.Status,
	})
	if err != nil {
		writePeopleError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, created)
}

func (s *Server) listDepartments(w http.ResponseWriter, r *http.Request, authentication guard.Authentication) {
	if s.people == nil {
		writeError(w, r, http.StatusServiceUnavailable, "repository_unavailable", "people directory unavailable")
		return
	}
	visibility, ok := s.directoryVisibility(w, r, authentication)
	if !ok {
		return
	}
	items, err := s.people.ListDepartments(r.Context(), visibility)
	if err != nil {
		writePeopleError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) createDepartment(w http.ResponseWriter, r *http.Request, _ guard.Authentication) {
	if s.people == nil {
		writeError(w, r, http.StatusServiceUnavailable, "repository_unavailable", "people directory unavailable")
		return
	}
	var input struct {
		Name   string              `json:"name"`
		SiteID string              `json:"siteId"`
		Status people.RecordStatus `json:"status"`
	}
	if err := decodeJSON(w, r, 32<<10, &input); err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_request", "invalid department payload")
		return
	}
	created, err := s.people.CreateDepartment(r.Context(), people.CreateDepartmentInput{
		Name: input.Name, SiteID: input.SiteID, Status: input.Status,
	})
	if err != nil {
		writePeopleError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, created)
}

func (s *Server) listIdentities(w http.ResponseWriter, r *http.Request, authentication guard.Authentication) {
	s.listIdentityCollection(w, r, authentication, "")
}

// listUsers preserves the initial REST collection as a person-only alias.
func (s *Server) listUsers(w http.ResponseWriter, r *http.Request, authentication guard.Authentication) {
	s.listIdentityCollection(w, r, authentication, people.IdentityPerson)
}

func (s *Server) listIdentityCollection(w http.ResponseWriter, r *http.Request, authentication guard.Authentication, forcedKind people.IdentityKind) {
	if s.people == nil {
		writeError(w, r, http.StatusServiceUnavailable, "repository_unavailable", "people directory unavailable")
		return
	}
	visibility, ok := s.directoryVisibility(w, r, authentication)
	if !ok {
		return
	}
	query, err := identityQueryFromRequest(r)
	if err != nil {
		writeError(w, r, http.StatusBadRequest, "validation_failed", "directory filters are invalid")
		return
	}
	if forcedKind != "" {
		query.Kind = forcedKind
	}
	items, err := s.people.SearchIdentities(r.Context(), query, visibility)
	if err != nil {
		writePeopleError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) createIdentity(w http.ResponseWriter, r *http.Request, _ guard.Authentication) {
	if s.people == nil {
		writeError(w, r, http.StatusServiceUnavailable, "repository_unavailable", "people directory unavailable")
		return
	}
	var input struct {
		Kind            people.IdentityKind `json:"kind"`
		DisplayName     string              `json:"displayName"`
		Email           string              `json:"email"`
		DepartmentID    string              `json:"departmentId"`
		SiteID          string              `json:"siteId"`
		Status          people.RecordStatus `json:"status"`
		Provider        string              `json:"provider"`
		ProviderSubject string              `json:"providerSubject"`
	}
	if err := decodeJSON(w, r, 32<<10, &input); err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_request", "invalid identity payload")
		return
	}
	created, err := s.people.CreateIdentity(r.Context(), people.CreateIdentityInput{
		Kind: input.Kind, DisplayName: input.DisplayName, Email: input.Email,
		DepartmentID: input.DepartmentID, SiteID: input.SiteID, Status: input.Status,
		Provider: input.Provider, ProviderSubject: input.ProviderSubject,
	})
	if err != nil {
		writePeopleError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, created)
}

func (s *Server) listAssetAssignments(w http.ResponseWriter, r *http.Request, authentication guard.Authentication) {
	if s.people == nil {
		writeError(w, r, http.StatusServiceUnavailable, "repository_unavailable", "people directory unavailable")
		return
	}
	visibility, ok := s.directoryVisibility(w, r, authentication)
	if !ok {
		return
	}
	items, err := s.people.ListAssetAssignments(r.Context(), r.PathValue("assetID"), visibility)
	if err != nil {
		writePeopleError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) createAssetAssignment(w http.ResponseWriter, r *http.Request, authentication guard.Authentication) {
	if s.people == nil {
		writeError(w, r, http.StatusServiceUnavailable, "repository_unavailable", "people directory unavailable")
		return
	}
	if !s.requireOrganizationPermission(w, r, authentication, guard.PermissionAssetsWrite) {
		return
	}
	if !s.requireResourceWrite(w, r, authentication, "asset", r.PathValue("assetID")) {
		return
	}
	var input struct {
		AssigneeKind  people.AssigneeKind   `json:"assigneeKind"`
		AssigneeID    string                `json:"assigneeId"`
		Role          people.AssignmentRole `json:"role"`
		EffectiveFrom *time.Time            `json:"effectiveFrom"`
	}
	if err := decodeJSON(w, r, 32<<10, &input); err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_request", "invalid asset assignment payload")
		return
	}
	effectiveFrom := time.Time{}
	if input.EffectiveFrom != nil {
		effectiveFrom = input.EffectiveFrom.UTC()
	}
	created, err := s.people.CreateAssetAssignment(r.Context(), people.CreateAssetAssignmentInput{
		AssetID: r.PathValue("assetID"), AssigneeKind: input.AssigneeKind,
		AssigneeID: input.AssigneeID, Role: input.Role, EffectiveFrom: effectiveFrom,
	})
	if err != nil {
		writePeopleError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, created)
}

func (s *Server) endAssetAssignment(w http.ResponseWriter, r *http.Request, authentication guard.Authentication) {
	if s.people == nil {
		writeError(w, r, http.StatusServiceUnavailable, "repository_unavailable", "people directory unavailable")
		return
	}
	if !s.requireOrganizationPermission(w, r, authentication, guard.PermissionAssetsWrite) {
		return
	}
	if !s.requireResourceWrite(w, r, authentication, "asset", r.PathValue("assetID")) {
		return
	}
	var input struct {
		EffectiveTo *time.Time `json:"effectiveTo"`
	}
	if err := decodeJSON(w, r, 16<<10, &input); err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_request", "invalid assignment end payload")
		return
	}
	effectiveTo := time.Time{}
	if input.EffectiveTo != nil {
		effectiveTo = input.EffectiveTo.UTC()
	}
	ended, err := s.people.EndAssetAssignment(r.Context(), people.EndAssetAssignmentInput{
		AssetID: r.PathValue("assetID"), AssignmentID: r.PathValue("assignmentID"), EffectiveTo: effectiveTo,
	})
	if err != nil {
		writePeopleError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, ended)
}

func (s *Server) listTags(w http.ResponseWriter, r *http.Request, _ guard.Authentication) {
	if !s.requireThreads(w, r) {
		return
	}
	items, err := s.threads.ListTags(r.Context())
	if err != nil {
		writeThreadsError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) getTag(w http.ResponseWriter, r *http.Request, _ guard.Authentication) {
	if !s.requireThreads(w, r) {
		return
	}
	item, err := s.threads.GetTag(r.Context(), r.PathValue("tagID"))
	if err != nil {
		writeThreadsError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) createTag(w http.ResponseWriter, r *http.Request, _ guard.Authentication) {
	if !s.requireThreads(w, r) {
		return
	}
	var input threads.CreateTagInput
	if err := decodeJSON(w, r, 32<<10, &input); err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_request", "invalid tag payload")
		return
	}
	item, err := s.threads.CreateTag(r.Context(), input)
	if err != nil {
		writeThreadsError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

func (s *Server) updateTag(w http.ResponseWriter, r *http.Request, authentication guard.Authentication) {
	if !s.requireThreads(w, r) || !s.requireResourceWrite(w, r, authentication, "tag", r.PathValue("tagID")) {
		return
	}
	var input threads.UpdateTagInput
	if err := decodeJSON(w, r, 32<<10, &input); err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_request", "invalid tag payload")
		return
	}
	input.ID = r.PathValue("tagID")
	item, err := s.threads.UpdateTag(r.Context(), input)
	if err != nil {
		writeThreadsError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) listGoals(w http.ResponseWriter, r *http.Request, _ guard.Authentication) {
	if !s.requireThreads(w, r) {
		return
	}
	items, err := s.threads.ListGoals(r.Context())
	if err != nil {
		writeThreadsError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) getGoal(w http.ResponseWriter, r *http.Request, _ guard.Authentication) {
	if !s.requireThreads(w, r) {
		return
	}
	item, err := s.threads.GetGoal(r.Context(), r.PathValue("goalID"))
	if err != nil {
		writeThreadsError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) createGoal(w http.ResponseWriter, r *http.Request, _ guard.Authentication) {
	if !s.requireThreads(w, r) {
		return
	}
	var input threads.CreateGoalInput
	if err := decodeJSON(w, r, 32<<10, &input); err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_request", "invalid goal payload")
		return
	}
	item, err := s.threads.CreateGoal(r.Context(), input)
	if err != nil {
		writeThreadsError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

func (s *Server) updateGoal(w http.ResponseWriter, r *http.Request, authentication guard.Authentication) {
	if !s.requireThreads(w, r) || !s.requireResourceWrite(w, r, authentication, "goal", r.PathValue("goalID")) {
		return
	}
	var input threads.UpdateGoalInput
	if err := decodeJSON(w, r, 32<<10, &input); err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_request", "invalid goal payload")
		return
	}
	input.ID = r.PathValue("goalID")
	item, err := s.threads.UpdateGoal(r.Context(), input)
	if err != nil {
		writeThreadsError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) listEffectiveTags(w http.ResponseWriter, r *http.Request, _ guard.Authentication) {
	if !s.requireThreads(w, r) {
		return
	}
	items, err := s.threads.EvaluateTags(r.Context(), threads.TargetType(r.PathValue("targetType")), r.PathValue("targetID"))
	if err != nil {
		writeThreadsError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) setTagRule(w http.ResponseWriter, r *http.Request, authentication guard.Authentication) {
	if !s.requireThreads(w, r) || !s.requireResourceWrite(w, r, authentication, r.PathValue("targetType"), r.PathValue("targetID")) {
		return
	}
	var input struct {
		Mode     threads.RuleMode `json:"mode"`
		Revision int64            `json:"revision"`
	}
	if err := decodeJSON(w, r, 16<<10, &input); err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_request", "invalid tag rule payload")
		return
	}
	item, err := s.threads.SetTagRule(r.Context(), threads.SetTagRuleInput{
		TargetType: threads.TargetType(r.PathValue("targetType")), TargetID: r.PathValue("targetID"),
		TagID: r.PathValue("tagID"), Mode: input.Mode, Revision: input.Revision,
	})
	if err != nil {
		writeThreadsError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) deleteTagRule(w http.ResponseWriter, r *http.Request, authentication guard.Authentication) {
	if !s.requireThreads(w, r) || !s.requireResourceWrite(w, r, authentication, r.PathValue("targetType"), r.PathValue("targetID")) {
		return
	}
	revision, err := positiveRevision(r)
	if err != nil {
		writeError(w, r, http.StatusBadRequest, "validation_failed", "tag rule revision is required")
		return
	}
	err = s.threads.DeleteTagRule(r.Context(), threads.TargetType(r.PathValue("targetType")), r.PathValue("targetID"), r.PathValue("tagID"), revision)
	if err != nil {
		writeThreadsError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) listGoalLinks(w http.ResponseWriter, r *http.Request, _ guard.Authentication) {
	if !s.requireThreads(w, r) {
		return
	}
	items, err := s.threads.ListGoalLinks(r.Context(), threads.TargetType(r.PathValue("targetType")), r.PathValue("targetID"))
	if err != nil {
		writeThreadsError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) linkGoal(w http.ResponseWriter, r *http.Request, authentication guard.Authentication) {
	if !s.requireThreads(w, r) || !s.requireResourceWrite(w, r, authentication, r.PathValue("targetType"), r.PathValue("targetID")) {
		return
	}
	item, err := s.threads.LinkGoal(r.Context(), threads.LinkGoalInput{
		GoalID: r.PathValue("goalID"), TargetType: threads.TargetType(r.PathValue("targetType")), TargetID: r.PathValue("targetID"),
	})
	if err != nil {
		writeThreadsError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) unlinkGoal(w http.ResponseWriter, r *http.Request, authentication guard.Authentication) {
	if !s.requireThreads(w, r) || !s.requireResourceWrite(w, r, authentication, r.PathValue("targetType"), r.PathValue("targetID")) {
		return
	}
	err := s.threads.UnlinkGoal(r.Context(), threads.TargetType(r.PathValue("targetType")), r.PathValue("targetID"), r.PathValue("goalID"))
	if err != nil {
		writeThreadsError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) requireThreads(w http.ResponseWriter, r *http.Request) bool {
	if s.threads != nil {
		return true
	}
	writeError(w, r, http.StatusServiceUnavailable, "repository_unavailable", "Threads service unavailable")
	return false
}

func positiveRevision(r *http.Request) (int64, error) {
	value, err := strconv.ParseInt(strings.TrimSpace(r.URL.Query().Get("revision")), 10, 64)
	if err != nil || value < 1 {
		return 0, errors.New("revision must be positive")
	}
	return value, nil
}

func (s *Server) createAsset(w http.ResponseWriter, r *http.Request, _ guard.Authentication) {
	if s.atlas == nil {
		writeError(w, r, http.StatusServiceUnavailable, "repository_unavailable", "asset repository unavailable")
		return
	}
	var input atlas.CreateAssetInput
	if err := decodeJSON(w, r, 64<<10, &input); err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_request", "invalid asset payload")
		return
	}
	created, err := s.atlas.CreateAsset(r.Context(), input)
	if err != nil {
		writeAtlasError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, created)
}

func (s *Server) updateAsset(w http.ResponseWriter, r *http.Request, authentication guard.Authentication) {
	if s.atlas == nil {
		writeError(w, r, http.StatusServiceUnavailable, "repository_unavailable", "asset repository unavailable")
		return
	}
	assetID := r.PathValue("assetID")
	if !s.requireResourceWrite(w, r, authentication, "asset", assetID) {
		return
	}
	var input atlas.UpdateAssetInput
	if err := decodeJSON(w, r, 64<<10, &input); err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_request", "invalid asset payload")
		return
	}
	input.ID = assetID
	updated, err := s.atlas.UpdateAsset(r.Context(), input)
	if err != nil {
		writeAtlasError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

func (s *Server) listAssetLifecycle(w http.ResponseWriter, r *http.Request, _ guard.Authentication) {
	if s.atlas == nil {
		writeError(w, r, http.StatusServiceUnavailable, "repository_unavailable", "asset repository unavailable")
		return
	}
	items, err := s.atlas.ListAssetLifecycle(r.Context(), r.PathValue("assetID"))
	if err != nil {
		writeAtlasError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func assetQueryFromRequest(r *http.Request) (atlas.Query, error) {
	values := r.URL.Query()
	limit := 0
	if rawLimit := strings.TrimSpace(values.Get("limit")); rawLimit != "" {
		parsed, err := strconv.Atoi(rawLimit)
		if err != nil {
			return atlas.Query{}, err
		}
		limit = parsed
	}
	return atlas.Query{
		Search: values.Get("q"), Kind: values.Get("kind"), Status: values.Get("status"),
		SiteID: values.Get("siteId"), DepartmentID: values.Get("departmentId"), UserID: values.Get("userId"),
		Limit: limit,
	}, nil
}

func writeAtlasError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, atlas.ErrInvalidInput):
		writeError(w, r, http.StatusBadRequest, "validation_failed", "asset details or filters are invalid")
	case errors.Is(err, atlas.ErrNotFound):
		writeError(w, r, http.StatusNotFound, "not_found", "asset not found")
	case errors.Is(err, atlas.ErrReferenceMissing):
		writeError(w, r, http.StatusBadRequest, "reference_missing", "a selected asset location, department, or user is unavailable")
	case errors.Is(err, atlas.ErrConflict):
		writeError(w, r, http.StatusConflict, "conflict", "asset identity or revision conflicts with current data")
	default:
		writeError(w, r, http.StatusInternalServerError, "repository_error", "the asset operation could not be completed")
	}
}

func writeThreadsError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, threads.ErrInvalidInput):
		writeError(w, r, http.StatusBadRequest, "validation_failed", "tag, goal, target, or revision details are invalid")
	case errors.Is(err, threads.ErrNotFound):
		writeError(w, r, http.StatusNotFound, "not_found", "the requested tag, goal, or target was not found")
	case errors.Is(err, threads.ErrCycle):
		writeError(w, r, http.StatusConflict, "hierarchy_cycle", "the selected parent would create a hierarchy cycle")
	case errors.Is(err, threads.ErrConflict):
		writeError(w, r, http.StatusConflict, "conflict", "the tag, goal, or relationship conflicts with current data")
	default:
		writeError(w, r, http.StatusInternalServerError, "repository_error", "the Threads operation could not be completed")
	}
}

func identityQueryFromRequest(r *http.Request) (people.IdentityQuery, error) {
	values := r.URL.Query()
	limit := 0
	if rawLimit := strings.TrimSpace(values.Get("limit")); rawLimit != "" {
		parsed, err := strconv.Atoi(rawLimit)
		if err != nil {
			return people.IdentityQuery{}, err
		}
		limit = parsed
	}
	return people.IdentityQuery{
		Search:       values.Get("q"),
		Kind:         people.IdentityKind(values.Get("kind")),
		Status:       people.RecordStatus(values.Get("status")),
		DepartmentID: values.Get("departmentId"),
		SiteID:       values.Get("siteId"),
		Limit:        limit,
	}, nil
}

func (s *Server) directoryVisibility(w http.ResponseWriter, r *http.Request, authentication guard.Authentication) (people.Visibility, bool) {
	visibility := people.Visibility{}
	for _, grant := range authentication.Grants {
		if grant.Permission != guard.PermissionDirectoryRead || grant.Scope.OrganizationID != s.organization.ID {
			continue
		}
		switch grant.Scope.Kind {
		case guard.ScopeOrganization:
			return people.Visibility{All: true}, true
		case guard.ScopeDepartment:
			visibility.DepartmentIDs = append(visibility.DepartmentIDs, grant.Scope.ResourceID)
		case guard.ScopeSite:
			visibility.SiteIDs = append(visibility.SiteIDs, grant.Scope.ResourceID)
		}
	}
	if visibility.Empty() {
		_ = s.guard.CheckPermission(r.Context(), authentication, guard.PermissionDirectoryRead, guard.Scope{
			Kind: guard.ScopeOrganization, OrganizationID: s.organization.ID, ResourceID: s.organization.ID,
		})
		writeError(w, r, http.StatusForbidden, "permission_denied", "directory permission is required for this operation")
		return people.Visibility{}, false
	}
	return visibility, true
}

func (s *Server) requireOrganizationPermission(w http.ResponseWriter, r *http.Request, authentication guard.Authentication, permission guard.Permission) bool {
	err := s.guard.CheckPermission(r.Context(), authentication, permission, guard.Scope{
		Kind: guard.ScopeOrganization, OrganizationID: s.organization.ID, ResourceID: s.organization.ID,
	})
	if err != nil {
		writeError(w, r, http.StatusForbidden, "permission_denied", "permission is required for this operation")
		return false
	}
	return true
}

func (s *Server) requireResourceWrite(w http.ResponseWriter, r *http.Request, authentication guard.Authentication, resourceType, resourceID string) bool {
	err := s.guard.CheckResourceWrite(r.Context(), authentication, resourceType, resourceID)
	switch {
	case err == nil:
		return true
	case errors.Is(err, guard.ErrInvalidInput):
		writeError(w, r, http.StatusBadRequest, "validation_failed", "resource identity is invalid")
	case errors.Is(err, guard.ErrResourceWriteLocked):
		writeError(w, r, http.StatusLocked, "ownership_locked", "claim local ownership before changing this imported resource")
	default:
		writeError(w, r, http.StatusInternalServerError, "guard_error", "resource ownership could not be verified")
	}
	return false
}

func (s *Server) protected(permission guard.Permission, requireCSRF bool, next authenticatedHandler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.noStore(w)
		if s.guard == nil {
			writeError(w, r, http.StatusServiceUnavailable, "authentication_unavailable", "authentication service unavailable")
			return
		}
		cookie, err := r.Cookie(s.sessionCookieName())
		if err != nil || cookie.Value == "" {
			writeError(w, r, http.StatusUnauthorized, "authentication_required", "sign in is required")
			return
		}
		authentication, err := s.guard.AuthenticateSession(r.Context(), cookie.Value)
		if err != nil {
			s.clearSessionCookie(w)
			writeError(w, r, http.StatusUnauthorized, "invalid_session", "the session is invalid or expired")
			return
		}
		if scope, ok := foundation.ScopeFromContext(r.Context()); ok {
			scope.ActorID = authentication.Principal.Subject
			r = r.WithContext(foundation.WithScope(r.Context(), scope))
		}
		if requireCSRF {
			if !s.trustedBrowserRequest(r) {
				writeError(w, r, http.StatusForbidden, "origin_denied", "request origin is not allowed")
				return
			}
			if err := s.guard.ValidateCSRF(authentication, r.Header.Get(csrfHeader)); err != nil {
				writeError(w, r, http.StatusForbidden, "csrf_failed", "request verification failed")
				return
			}
		}
		if permission != "" {
			err := s.guard.CheckPermission(r.Context(), authentication, permission, guard.Scope{
				Kind:           guard.ScopeOrganization,
				OrganizationID: s.organization.ID,
				ResourceID:     s.organization.ID,
			})
			if err != nil {
				writeError(w, r, http.StatusForbidden, "permission_denied", "permission is required for this operation")
				return
			}
		}
		next(w, r, authentication)
	})
}

func (s *Server) writeAuthenticatedSession(w http.ResponseWriter, status int, credentials guard.SessionCredentials) {
	s.setSessionCookie(w, credentials)
	writeJSON(w, status, sessionResponse(credentials.Authentication, credentials.CSRFToken))
}

func (s *Server) setSessionCookie(w http.ResponseWriter, credentials guard.SessionCredentials) {
	http.SetCookie(w, &http.Cookie{
		Name:     s.sessionCookieName(),
		Value:    credentials.Token,
		Path:     "/",
		Expires:  credentials.Authentication.Session.ExpiresAt,
		MaxAge:   int(time.Until(credentials.Authentication.Session.ExpiresAt).Seconds()),
		HttpOnly: true,
		Secure:   s.sessionCookieSecure,
		SameSite: http.SameSiteLaxMode,
	})
}

func sessionResponse(authentication guard.Authentication, csrfToken string) map[string]any {
	return map[string]any{
		"principal":   authentication.Principal,
		"permissions": organizationPermissions(authentication),
		"csrfToken":   csrfToken,
		"expiresAt":   authentication.Session.ExpiresAt,
	}
}

func organizationPermissions(authentication guard.Authentication) []string {
	seen := make(map[guard.Permission]struct{})
	for _, grant := range authentication.Grants {
		if grant.Scope.Kind == guard.ScopeOrganization && grant.Scope.OrganizationID == authentication.Principal.OrganizationID {
			seen[grant.Permission] = struct{}{}
		}
	}
	permissions := make([]string, 0, len(seen))
	for permission := range seen {
		permissions = append(permissions, string(permission))
	}
	sort.Strings(permissions)
	return permissions
}

func (s *Server) clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     s.sessionCookieName(),
		Value:    "",
		Path:     "/",
		Expires:  time.Unix(1, 0).UTC(),
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   s.sessionCookieSecure,
		SameSite: http.SameSiteLaxMode,
	})
}

func (s *Server) sessionCookieName() string {
	if s.sessionCookieSecure {
		return secureSessionName
	}
	return localSessionName
}

func (s *Server) oidcTransactionCookieName() string {
	if s.sessionCookieSecure {
		return secureOIDCTransactionName
	}
	return localOIDCTransactionName
}

func (s *Server) clearOIDCTransactionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     s.oidcTransactionCookieName(),
		Value:    "",
		Path:     "/",
		Expires:  time.Unix(1, 0).UTC(),
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   s.sessionCookieSecure,
		SameSite: http.SameSiteLaxMode,
	})
}

func (s *Server) trustedExternalAuthStart(r *http.Request) bool {
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin != "" {
		return s.allowedOrigin != "" && origin == s.allowedOrigin
	}
	switch strings.ToLower(strings.TrimSpace(r.Header.Get("Sec-Fetch-Site"))) {
	case "same-origin", "none":
		return true
	case "same-site", "cross-site":
		return false
	}
	return isLoopbackOrigin(s.allowedOrigin) && isLoopbackRemote(r.RemoteAddr)
}

func (s *Server) trustedBrowserRequest(r *http.Request) bool {
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin != "" {
		return s.allowedOrigin != "" && origin == s.allowedOrigin
	}
	if strings.EqualFold(r.Header.Get("Sec-Fetch-Site"), "cross-site") {
		return false
	}
	return isLoopbackRemote(r.RemoteAddr)
}

func (s *Server) correlation(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		correlationID := strings.TrimSpace(r.Header.Get("X-Correlation-ID"))
		if !correlationIDPattern.MatchString(correlationID) {
			generated, err := foundation.NewCorrelationID()
			if err != nil {
				writeJSON(w, http.StatusInternalServerError, map[string]any{
					"error": map[string]string{"code": "correlation_failed", "message": "unable to initialize request context"},
				})
				return
			}
			correlationID = generated
		}
		scope := foundation.Scope{
			OrganizationID: s.organization.ID,
			ActorID:        "anonymous",
			CorrelationID:  correlationID,
		}
		w.Header().Set("X-Correlation-ID", correlationID)
		next.ServeHTTP(w, r.WithContext(foundation.WithScope(r.Context(), scope)))
	})
}

func (s *Server) securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Security-Policy", "default-src 'self'; connect-src 'self'; img-src 'self' data:; style-src 'self'; script-src 'self'; object-src 'none'; base-uri 'none'; frame-ancestors 'none'; form-action 'self'")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Permissions-Policy", "camera=(), geolocation=(), microphone=()")
		next.ServeHTTP(w, r)
	})
}

func (s *Server) cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		originAllowed := s.allowedOrigin != "" && origin == s.allowedOrigin
		if s.allowedOrigin != "" {
			w.Header().Add("Vary", "Origin")
		}
		if originAllowed {
			w.Header().Set("Access-Control-Allow-Origin", s.allowedOrigin)
			w.Header().Set("Access-Control-Allow-Credentials", "true")
			w.Header().Set("Access-Control-Expose-Headers", "X-Correlation-ID")
		}
		if r.Method == http.MethodOptions {
			if !originAllowed {
				writeError(w, r, http.StatusForbidden, "origin_denied", "request origin is not allowed")
				return
			}
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-CSRF-Token, X-Correlation-ID")
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) noStore(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
}

func decodeJSON(w http.ResponseWriter, r *http.Request, maximumBytes int64, destination any) error {
	contentType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || contentType != "application/json" {
		return errors.New("content type must be application/json")
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, maximumBytes))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("request body must contain exactly one JSON document")
	}
	return nil
}

func remoteHost(remoteAddress string) string {
	host, _, err := net.SplitHostPort(remoteAddress)
	if err != nil {
		return "unknown"
	}
	return host
}

func isLoopbackRemote(remoteAddress string) bool {
	host := remoteHost(remoteAddress)
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func isLoopbackOrigin(rawOrigin string) bool {
	origin, err := url.Parse(rawOrigin)
	if err != nil {
		return false
	}
	host := origin.Hostname()
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, r *http.Request, status int, code, message string) {
	correlationID := ""
	if scope, ok := foundation.ScopeFromContext(r.Context()); ok {
		correlationID = scope.CorrelationID
	}
	writeJSON(w, status, map[string]any{
		"error": map[string]string{
			"code":          code,
			"message":       message,
			"correlationId": correlationID,
		},
	})
}

func writePeopleError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, people.ErrInvalidInput):
		writeError(w, r, http.StatusBadRequest, "validation_failed", "people directory input is invalid")
	case errors.Is(err, people.ErrNotFound), errors.Is(err, people.ErrReferenceMissing):
		writeError(w, r, http.StatusNotFound, "not_found", "the requested directory reference was not found")
	case errors.Is(err, people.ErrConflict):
		writeError(w, r, http.StatusConflict, "conflict", "the directory operation conflicts with existing data")
	case errors.Is(err, people.ErrScopeRequired):
		writeError(w, r, http.StatusForbidden, "permission_denied", "directory scope is required for this operation")
	default:
		writeError(w, r, http.StatusInternalServerError, "repository_error", "the people directory operation could not be completed")
	}
}
