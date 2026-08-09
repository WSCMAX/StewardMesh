// Package directoryexpansion contains provider-neutral location, import, and
// relationship contracts used by optional directory integrations.
package directoryexpansion

import (
	"context"
	"time"
)

type Address struct {
	Line1      string `json:"line1,omitempty"`
	Line2      string `json:"line2,omitempty"`
	City       string `json:"city,omitempty"`
	Region     string `json:"region,omitempty"`
	PostalCode string `json:"postalCode,omitempty"`
	Country    string `json:"country,omitempty"`
}

type Building struct {
	ID, OrganizationID, SiteID, Name string
	Status                           string
	CreatedAt, UpdatedAt             time.Time
}
type Room struct {
	ID, OrganizationID, SiteID, BuildingID, Number, Name string
	Status                                               string
	CreatedAt, UpdatedAt                                 time.Time
}

type Provider string

const (
	ProviderEntra      Provider = "entra"
	ProviderSailPoint  Provider = "sailpoint"
	ProviderGrouper    Provider = "grouper"
	ProviderPeopleSoft Provider = "peoplesoft"
)

type ImportRecord struct {
	Provider                                   Provider
	SourceID, Kind, DisplayName, Email, Status string
	Attributes                                 map[string]string
	GroupIDs                                   []string
}
type ImportRequest struct {
	Provider Provider
	DryRun   bool
	Records  []ImportRecord
}
type ImportResult struct {
	Provider                                            Provider
	BatchID                                             string
	Created, Updated, Unchanged, Deactivated, Conflicts int
	Errors                                              []string
	Applied                                             bool
}

type Connector interface {
	Provider() Provider
	Pull(context.Context) ([]ImportRecord, error)
}
type Importer interface {
	Import(context.Context, ImportRequest) (ImportResult, error)
}

type Node struct {
	ID, Kind, Label string
	Attributes      map[string]string `json:"attributes,omitempty"`
}
type Edge struct {
	ID, From, To, Kind string
	Attributes         map[string]string `json:"attributes,omitempty"`
}
type Graph struct {
	Nodes []Node `json:"nodes"`
	Edges []Edge `json:"edges"`
}
type GraphQuery struct {
	Search, Kind, Relationship string
	Limit                      int
}
type GraphStore interface {
	Graph(context.Context, GraphQuery) (Graph, error)
}
