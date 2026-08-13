package domain

import "time"

// Requirements: REQ-ATLAS-001, REQ-ATLAS-MODELS-001.
type Asset struct {
	ID              string             `json:"id"`
	OrganizationID  string             `json:"organizationId"`
	ModelID         string             `json:"modelId,omitempty"`
	ModelContext    *AssetModelContext `json:"modelContext,omitempty"`
	Name            string             `json:"name"`
	Kind            string             `json:"kind"`
	AssetTag        string             `json:"assetTag,omitempty"`
	SerialNumber    string             `json:"serialNumber,omitempty"`
	Hostname        string             `json:"hostname,omitempty"`
	DeploymentNotes string             `json:"deploymentNotes,omitempty"`
	SiteID          string             `json:"siteId,omitempty"`
	BuildingID      string             `json:"buildingId,omitempty"`
	RoomID          string             `json:"roomId,omitempty"`
	DepartmentID    string             `json:"departmentId,omitempty"`
	UserID          string             `json:"userId,omitempty"`
	Status          string             `json:"status"`
	PurchaseDate    *time.Time         `json:"purchaseDate,omitempty"`
	Revision        int64              `json:"revision"`
	CreatedAt       time.Time          `json:"createdAt"`
	UpdatedAt       time.Time          `json:"updatedAt"`
}

// AssetModelContext is the immutable model-default snapshot captured when an
// asset is linked to a model. Later model edits never rewrite this provenance.
type AssetModelContext struct {
	Manufacturer        string            `json:"manufacturer"`
	Name                string            `json:"name"`
	ModelNumber         string            `json:"modelNumber,omitempty"`
	Kind                string            `json:"kind"`
	VendorIdentifier    string            `json:"vendorIdentifier,omitempty"`
	Specifications      map[string]string `json:"specifications,omitempty"`
	SupportURL          string            `json:"supportUrl,omitempty"`
	WarrantyMonths      int               `json:"warrantyMonths,omitempty"`
	UsefulLifeMonths    int               `json:"usefulLifeMonths,omitempty"`
	SourceSystemID      string            `json:"sourceSystemId,omitempty"`
	SourceRecordID      string            `json:"sourceRecordId,omitempty"`
	ModelRevision       int64             `json:"modelRevision"`
	DefaultsEffectiveAt time.Time         `json:"defaultsEffectiveAt"`
	AppliedAt           time.Time         `json:"appliedAt"`
	Overrides           []string          `json:"overrides"`
}

type AssetModel struct {
	ID               string            `json:"id"`
	OrganizationID   string            `json:"organizationId"`
	Manufacturer     string            `json:"manufacturer"`
	Name             string            `json:"name"`
	ModelNumber      string            `json:"modelNumber,omitempty"`
	Kind             string            `json:"kind"`
	VendorIdentifier string            `json:"vendorIdentifier,omitempty"`
	Specifications   map[string]string `json:"specifications,omitempty"`
	SupportURL       string            `json:"supportUrl,omitempty"`
	WarrantyMonths   int               `json:"warrantyMonths,omitempty"`
	UsefulLifeMonths int               `json:"usefulLifeMonths,omitempty"`
	Status           string            `json:"status"`
	SourceSystemID   string            `json:"sourceSystemId,omitempty"`
	SourceRecordID   string            `json:"sourceRecordId,omitempty"`
	InstanceCount    int               `json:"instanceCount"`
	Revision         int64             `json:"revision"`
	CreatedAt        time.Time         `json:"createdAt"`
	UpdatedAt        time.Time         `json:"updatedAt"`
}

type AssetLifecycleEvent struct {
	ID             string    `json:"id"`
	OrganizationID string    `json:"organizationId"`
	AssetID        string    `json:"assetId"`
	FromStatus     string    `json:"fromStatus,omitempty"`
	ToStatus       string    `json:"toStatus"`
	Note           string    `json:"note,omitempty"`
	Revision       int64     `json:"revision"`
	ActorID        string    `json:"actorId"`
	OccurredAt     time.Time `json:"occurredAt"`
}
