package domain

import "time"

type Asset struct {
	ID             string     `json:"id"`
	OrganizationID string     `json:"organizationId"`
	Name           string     `json:"name"`
	Kind           string     `json:"kind"`
	AssetTag       string     `json:"assetTag,omitempty"`
	SerialNumber   string     `json:"serialNumber,omitempty"`
	Hostname       string     `json:"hostname,omitempty"`
	SiteID         string     `json:"siteId,omitempty"`
	BuildingID     string     `json:"buildingId,omitempty"`
	RoomID         string     `json:"roomId,omitempty"`
	DepartmentID   string     `json:"departmentId,omitempty"`
	UserID         string     `json:"userId,omitempty"`
	Status         string     `json:"status"`
	PurchaseDate   *time.Time `json:"purchaseDate,omitempty"`
	Revision       int64      `json:"revision"`
	CreatedAt      time.Time  `json:"createdAt"`
	UpdatedAt      time.Time  `json:"updatedAt"`
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

type Tag struct {
	ID               string `json:"id"`
	Name             string `json:"name"`
	ParentID         string `json:"parentId,omitempty"`
	InheritByDefault bool   `json:"inheritByDefault"`
}

type Goal struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}
