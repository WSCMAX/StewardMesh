package exchange

// Requirements: REQ-EXCHANGE-001, REQ-PEOPLE-001, REQ-PATTERNS-001. Features: migration.packages, identity.directory, templates.schemas. GitHub: #9.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/maxlemke/stewardmesh/internal/people"
)

var peopleRecordTypes = []string{"people.site", "people.building", "people.room", "people.department", "people.identity", "people.assignment"}

type PeopleProvider struct {
	service  *people.Service
	importer people.ExchangeImporter
}

type peopleSitePayload struct {
	Name       string `json:"name"`
	Address1   string `json:"addressLine1,omitempty"`
	Address2   string `json:"addressLine2,omitempty"`
	City       string `json:"city,omitempty"`
	Region     string `json:"region,omitempty"`
	PostalCode string `json:"postalCode,omitempty"`
	Country    string `json:"country,omitempty"`
	Status     string `json:"status"`
	CreatedAt  string `json:"createdAt"`
	UpdatedAt  string `json:"updatedAt"`
}

type peopleBuildingPayload struct {
	SiteID    string `json:"siteId"`
	Name      string `json:"name"`
	Status    string `json:"status"`
	CreatedAt string `json:"createdAt"`
	UpdatedAt string `json:"updatedAt"`
}

type peopleRoomPayload struct {
	SiteID     string `json:"siteId"`
	BuildingID string `json:"buildingId"`
	Number     string `json:"number"`
	Name       string `json:"name,omitempty"`
	Status     string `json:"status"`
	CreatedAt  string `json:"createdAt"`
	UpdatedAt  string `json:"updatedAt"`
}

type peopleDepartmentPayload struct {
	Name      string `json:"name"`
	SiteID    string `json:"siteId,omitempty"`
	Status    string `json:"status"`
	CreatedAt string `json:"createdAt"`
	UpdatedAt string `json:"updatedAt"`
}

type peopleIdentityPayload struct {
	Kind            string `json:"kind"`
	DisplayName     string `json:"displayName"`
	Email           string `json:"email,omitempty"`
	DepartmentID    string `json:"departmentId,omitempty"`
	SiteID          string `json:"siteId,omitempty"`
	Status          string `json:"status"`
	Provider        string `json:"provider,omitempty"`
	ProviderSubject string `json:"providerSubject,omitempty"`
	CreatedAt       string `json:"createdAt"`
	UpdatedAt       string `json:"updatedAt"`
}

type peopleAssignmentPayload struct {
	AssetID       string `json:"assetId"`
	AssigneeKind  string `json:"assigneeKind"`
	AssigneeID    string `json:"assigneeId"`
	Role          string `json:"role"`
	EffectiveFrom string `json:"effectiveFrom"`
	EffectiveTo   string `json:"effectiveTo,omitempty"`
	CreatedAt     string `json:"createdAt"`
}

func NewPeopleProvider(service *people.Service, importer people.ExchangeImporter) (*PeopleProvider, error) {
	if service == nil || importer == nil || !service.OwnsExchangeImporter(importer) {
		return nil, errors.New("People service and its construction-time Exchange importer are required")
	}
	return &PeopleProvider{service: service, importer: importer}, nil
}

func (*PeopleProvider) Types() []string { return append([]string(nil), peopleRecordTypes...) }

func (p *PeopleProvider) ListRecords(ctx context.Context) ([]Record, error) {
	snapshot, err := p.service.ExchangeSnapshot(ctx, MaximumRecords)
	if errors.Is(err, people.ErrTooLarge) {
		return nil, ErrTooLarge
	}
	if err != nil {
		return nil, err
	}
	result := make([]Record, 0, len(snapshot.Sites)+len(snapshot.Buildings)+len(snapshot.Rooms)+len(snapshot.Departments)+len(snapshot.Identities)+len(snapshot.Assignments))
	appendRecord := func(recordType, id string, revision uint64, dependencies []Reference, payload any) error {
		encoded, err := json.Marshal(payload)
		if err != nil || len(encoded) == 0 || len(encoded) > MaximumPayloadBytes {
			return ErrInvalidInput
		}
		result = append(result, Record{Type: recordType, ID: id, Revision: int64(revision), Dependencies: normalizeReferences(dependencies), Ownership: OwnershipMetadata{State: "local"}, Payload: encoded})
		return nil
	}
	for _, item := range snapshot.Sites {
		if err := validatePortableInstants(2000, item.CreatedAt, item.UpdatedAt); err != nil {
			return nil, err
		}
		if err := appendRecord("people.site", item.ID, item.Revision, nil, peopleSitePayload{Name: item.Name, Address1: item.Address.Line1, Address2: item.Address.Line2, City: item.Address.City, Region: item.Address.Region, PostalCode: item.Address.PostalCode, Country: item.Address.Country, Status: string(item.Status), CreatedAt: peopleInstant(item.CreatedAt), UpdatedAt: peopleInstant(item.UpdatedAt)}); err != nil {
			return nil, err
		}
	}
	for _, item := range snapshot.Buildings {
		if err := validatePortableInstants(2000, item.CreatedAt, item.UpdatedAt); err != nil {
			return nil, err
		}
		if err := appendRecord("people.building", item.ID, item.Revision, []Reference{{Type: "people.site", ID: item.SiteID}}, peopleBuildingPayload{SiteID: item.SiteID, Name: item.Name, Status: string(item.Status), CreatedAt: peopleInstant(item.CreatedAt), UpdatedAt: peopleInstant(item.UpdatedAt)}); err != nil {
			return nil, err
		}
	}
	for _, item := range snapshot.Rooms {
		if err := validatePortableInstants(2000, item.CreatedAt, item.UpdatedAt); err != nil {
			return nil, err
		}
		if err := appendRecord("people.room", item.ID, item.Revision, []Reference{{Type: "people.site", ID: item.SiteID}, {Type: "people.building", ID: item.BuildingID}}, peopleRoomPayload{SiteID: item.SiteID, BuildingID: item.BuildingID, Number: item.Number, Name: item.Name, Status: string(item.Status), CreatedAt: peopleInstant(item.CreatedAt), UpdatedAt: peopleInstant(item.UpdatedAt)}); err != nil {
			return nil, err
		}
	}
	for _, item := range snapshot.Departments {
		if err := validatePortableInstants(2000, item.CreatedAt, item.UpdatedAt); err != nil {
			return nil, err
		}
		dependencies := []Reference{}
		if item.SiteID != "" {
			dependencies = append(dependencies, Reference{Type: "people.site", ID: item.SiteID})
		}
		if err := appendRecord("people.department", item.ID, item.Revision, dependencies, peopleDepartmentPayload{Name: item.Name, SiteID: item.SiteID, Status: string(item.Status), CreatedAt: peopleInstant(item.CreatedAt), UpdatedAt: peopleInstant(item.UpdatedAt)}); err != nil {
			return nil, err
		}
	}
	for _, item := range snapshot.Identities {
		if err := validatePortableInstants(2000, item.CreatedAt, item.UpdatedAt); err != nil {
			return nil, err
		}
		dependencies := []Reference{}
		if item.DepartmentID != "" {
			dependencies = append(dependencies, Reference{Type: "people.department", ID: item.DepartmentID})
		}
		if item.SiteID != "" {
			dependencies = append(dependencies, Reference{Type: "people.site", ID: item.SiteID})
		}
		if err := appendRecord("people.identity", item.ID, item.Revision, dependencies, peopleIdentityPayload{Kind: string(item.Kind), DisplayName: item.DisplayName, Email: item.Email, DepartmentID: item.DepartmentID, SiteID: item.SiteID, Status: string(item.Status), Provider: item.Provider, ProviderSubject: item.ProviderSubject, CreatedAt: peopleInstant(item.CreatedAt), UpdatedAt: peopleInstant(item.UpdatedAt)}); err != nil {
			return nil, err
		}
	}
	for _, item := range snapshot.Assignments {
		if err := validatePortableInstants(2000, item.EffectiveFrom, item.CreatedAt); err != nil {
			return nil, err
		}
		if err := validateOptionalPortableInstant(2000, item.EffectiveTo); err != nil {
			return nil, err
		}
		assigneeType := "people.identity"
		if item.AssigneeKind == people.AssigneeDepartment {
			assigneeType = "people.department"
		}
		dependencies := []Reference{{Type: "atlas.asset", ID: item.AssetID}, {Type: assigneeType, ID: item.AssigneeID}}
		if err := appendRecord("people.assignment", item.ID, 1, dependencies, peopleAssignmentPayload{AssetID: item.AssetID, AssigneeKind: string(item.AssigneeKind), AssigneeID: item.AssigneeID, Role: string(item.Role), EffectiveFrom: peopleInstant(item.EffectiveFrom), EffectiveTo: peopleOptionalInstant(item.EffectiveTo), CreatedAt: peopleInstant(item.CreatedAt)}); err != nil {
			return nil, err
		}
	}
	sort.Slice(result, func(i, j int) bool {
		return (Reference{Type: result[i].Type, ID: result[i].ID}).Key() < (Reference{Type: result[j].Type, ID: result[j].ID}).Key()
	})
	return result, nil
}

func (p *PeopleProvider) Exists(ctx context.Context, reference Reference) (bool, error) {
	var err error
	switch reference.Type {
	case "people.site":
		_, err = p.service.GetSite(ctx, reference.ID)
	case "people.building":
		_, err = p.service.GetBuilding(ctx, reference.ID)
	case "people.room":
		_, err = p.service.GetRoom(ctx, reference.ID)
	case "people.department":
		_, err = p.service.GetDepartment(ctx, reference.ID)
	case "people.identity":
		_, err = p.service.GetIdentity(ctx, reference.ID)
	case "people.assignment":
		_, err = p.service.GetAssetAssignment(ctx, reference.ID)
	default:
		return false, nil
	}
	if errors.Is(err, people.ErrNotFound) {
		return false, nil
	}
	return err == nil, err
}

func (p *PeopleProvider) ImportRecordExists(ctx context.Context, record Record, _ []byte) (bool, error) {
	candidate, dependencies, err := decodePeopleRecord(record)
	if err != nil || !slices.Equal(dependencies, record.Dependencies) {
		return false, ErrInvalidInput
	}
	switch item := candidate.(type) {
	case people.Site:
		current, err := p.service.GetSite(ctx, item.ID)
		return exactPeopleRecord(current, item, err, samePeopleSite)
	case people.Building:
		current, err := p.service.GetBuilding(ctx, item.ID)
		return exactPeopleRecord(current, item, err, samePeopleBuilding)
	case people.Room:
		current, err := p.service.GetRoom(ctx, item.ID)
		return exactPeopleRecord(current, item, err, samePeopleRoom)
	case people.Department:
		current, err := p.service.GetDepartment(ctx, item.ID)
		return exactPeopleRecord(current, item, err, samePeopleDepartment)
	case people.Identity:
		current, err := p.service.GetIdentity(ctx, item.ID)
		return exactPeopleRecord(current, item, err, samePeopleIdentity)
	case people.AssetAssignment:
		current, err := p.service.GetAssetAssignment(ctx, item.ID)
		return exactPeopleRecord(current, item, err, samePeopleAssignment)
	default:
		return false, ErrInvalidInput
	}
}

func (p *PeopleProvider) ImportRecord(ctx context.Context, operation ProviderImportOperation, _ string, record Record, _ []byte) (ProviderImportResult, error) {
	if !operation.ExpectedCreated {
		exact, err := p.ImportRecordExists(ctx, record, nil)
		if err != nil {
			return ProviderImportResult{}, err
		}
		if !exact {
			return ProviderImportResult{}, ErrConflict
		}
		return ProviderImportResult{Committed: true}, nil
	}
	candidate, dependencies, err := decodePeopleRecord(record)
	if err != nil || !slices.Equal(dependencies, record.Dependencies) {
		return ProviderImportResult{}, ErrInvalidInput
	}
	domainOperation := people.ExchangeImportOperation{Token: operation.Token, OccurredAt: operation.OccurredAt}
	var result people.ExchangeImportResult
	switch item := candidate.(type) {
	case people.Site:
		result, err = p.importer.ImportSite(ctx, domainOperation, item)
	case people.Building:
		result, err = p.importer.ImportBuilding(ctx, domainOperation, item)
	case people.Room:
		result, err = p.importer.ImportRoom(ctx, domainOperation, item)
	case people.Department:
		result, err = p.importer.ImportDepartment(ctx, domainOperation, item)
	case people.Identity:
		result, err = p.importer.ImportIdentity(ctx, domainOperation, item)
	case people.AssetAssignment:
		result, err = p.importer.ImportAssetAssignment(ctx, domainOperation, item)
	default:
		return ProviderImportResult{}, ErrInvalidInput
	}
	providerResult := ProviderImportResult{Committed: result.Committed, Created: result.Created}
	switch {
	case errors.Is(err, people.ErrInvalidInput):
		return providerResult, ErrInvalidInput
	case errors.Is(err, people.ErrConflict):
		return providerResult, ErrConflict
	case errors.Is(err, people.ErrNotFound), errors.Is(err, people.ErrReferenceMissing):
		return providerResult, ErrDependencyMissing
	default:
		return providerResult, err
	}
}

func decodePeopleRecord(record Record) (any, []Reference, error) {
	if record.Revision < 1 {
		return nil, nil, ErrInvalidInput
	}
	switch record.Type {
	case "people.site":
		payload, err := decodePeoplePayload[peopleSitePayload](record.Payload)
		createdAt, updatedAt, parseErr := parsePeopleStateTimes(payload.CreatedAt, payload.UpdatedAt)
		if err != nil || parseErr != nil || !canonicalPeopleSitePayload(payload) || !validPeopleRecordID(record.ID) {
			return nil, nil, ErrInvalidInput
		}
		return people.Site{ID: record.ID, Name: payload.Name, NormalizedName: strings.ToLower(payload.Name), Address: people.Address{Line1: payload.Address1, Line2: payload.Address2, City: payload.City, Region: payload.Region, PostalCode: payload.PostalCode, Country: payload.Country}, Status: people.RecordStatus(payload.Status), Revision: uint64(record.Revision), CreatedAt: createdAt, UpdatedAt: updatedAt}, []Reference{}, nil
	case "people.building":
		payload, err := decodePeoplePayload[peopleBuildingPayload](record.Payload)
		createdAt, updatedAt, parseErr := parsePeopleStateTimes(payload.CreatedAt, payload.UpdatedAt)
		if err != nil || parseErr != nil || !canonicalPeopleBuildingPayload(payload) || !validPeopleRecordID(record.ID) || !validPeopleRecordID(payload.SiteID) {
			return nil, nil, ErrInvalidInput
		}
		item := people.Building{ID: record.ID, SiteID: payload.SiteID, Name: payload.Name, NormalizedName: strings.ToLower(payload.Name), Status: people.RecordStatus(payload.Status), Revision: uint64(record.Revision), CreatedAt: createdAt, UpdatedAt: updatedAt}
		return item, []Reference{{Type: "people.site", ID: item.SiteID}}, nil
	case "people.room":
		payload, err := decodePeoplePayload[peopleRoomPayload](record.Payload)
		createdAt, updatedAt, parseErr := parsePeopleStateTimes(payload.CreatedAt, payload.UpdatedAt)
		if err != nil || parseErr != nil || !canonicalPeopleRoomPayload(payload) || !validPeopleRecordID(record.ID) || !validPeopleRecordID(payload.SiteID) || !validPeopleRecordID(payload.BuildingID) {
			return nil, nil, ErrInvalidInput
		}
		item := people.Room{ID: record.ID, SiteID: payload.SiteID, BuildingID: payload.BuildingID, Number: payload.Number, NormalizedNumber: strings.ToLower(payload.Number), Name: payload.Name, Status: people.RecordStatus(payload.Status), Revision: uint64(record.Revision), CreatedAt: createdAt, UpdatedAt: updatedAt}
		return item, normalizeReferences([]Reference{{Type: "people.site", ID: item.SiteID}, {Type: "people.building", ID: item.BuildingID}}), nil
	case "people.department":
		payload, err := decodePeoplePayload[peopleDepartmentPayload](record.Payload)
		createdAt, updatedAt, parseErr := parsePeopleStateTimes(payload.CreatedAt, payload.UpdatedAt)
		if err != nil || parseErr != nil || !canonicalPeopleDepartmentPayload(payload) || !validPeopleRecordID(record.ID) || payload.SiteID != "" && !validPeopleRecordID(payload.SiteID) {
			return nil, nil, ErrInvalidInput
		}
		item := people.Department{ID: record.ID, Name: payload.Name, NormalizedName: strings.ToLower(payload.Name), SiteID: payload.SiteID, Status: people.RecordStatus(payload.Status), Revision: uint64(record.Revision), CreatedAt: createdAt, UpdatedAt: updatedAt}
		dependencies := []Reference{}
		if item.SiteID != "" {
			dependencies = append(dependencies, Reference{Type: "people.site", ID: item.SiteID})
		}
		return item, dependencies, nil
	case "people.identity":
		payload, err := decodePeoplePayload[peopleIdentityPayload](record.Payload)
		createdAt, updatedAt, parseErr := parsePeopleStateTimes(payload.CreatedAt, payload.UpdatedAt)
		if err != nil || parseErr != nil || !canonicalPeopleIdentityPayload(payload) || !validPeopleRecordID(record.ID) ||
			payload.DepartmentID != "" && !validPeopleRecordID(payload.DepartmentID) || payload.SiteID != "" && !validPeopleRecordID(payload.SiteID) {
			return nil, nil, ErrInvalidInput
		}
		item := people.Identity{ID: record.ID, Kind: people.IdentityKind(payload.Kind), DisplayName: payload.DisplayName, NormalizedName: strings.ToLower(payload.DisplayName), Email: payload.Email, NormalizedEmail: payload.Email, DepartmentID: payload.DepartmentID, SiteID: payload.SiteID, Status: people.RecordStatus(payload.Status), Provider: payload.Provider, ProviderSubject: payload.ProviderSubject, Revision: uint64(record.Revision), CreatedAt: createdAt, UpdatedAt: updatedAt}
		dependencies := []Reference{}
		if item.DepartmentID != "" {
			dependencies = append(dependencies, Reference{Type: "people.department", ID: item.DepartmentID})
		}
		if item.SiteID != "" {
			dependencies = append(dependencies, Reference{Type: "people.site", ID: item.SiteID})
		}
		return item, normalizeReferences(dependencies), nil
	case "people.assignment":
		if record.Revision != 1 {
			return nil, nil, ErrInvalidInput
		}
		payload, err := decodePeoplePayload[peopleAssignmentPayload](record.Payload)
		effectiveFrom, parseFromErr := parsePeopleInstant(payload.EffectiveFrom)
		effectiveTo, parseToErr := parsePeopleOptionalInstant(payload.EffectiveTo)
		createdAt, parseCreatedErr := parsePeopleInstant(payload.CreatedAt)
		if err != nil || parseFromErr != nil || parseToErr != nil || parseCreatedErr != nil || !canonicalPeopleAssignmentPayload(payload) ||
			!validPeopleRecordID(record.ID) || !validPeopleStableID(payload.AssetID) || !validPeopleRecordID(payload.AssigneeID) || effectiveTo != nil && !effectiveTo.After(effectiveFrom) {
			return nil, nil, ErrInvalidInput
		}
		item := people.AssetAssignment{ID: record.ID, AssetID: payload.AssetID, AssigneeKind: people.AssigneeKind(payload.AssigneeKind), AssigneeID: payload.AssigneeID, Role: people.AssignmentRole(payload.Role), EffectiveFrom: effectiveFrom, EffectiveTo: effectiveTo, CreatedBy: "system:exchange", CreatedAt: createdAt}
		assigneeType := "people.identity"
		if item.AssigneeKind == people.AssigneeDepartment {
			assigneeType = "people.department"
		}
		return item, normalizeReferences([]Reference{{Type: "atlas.asset", ID: item.AssetID}, {Type: assigneeType, ID: item.AssigneeID}}), nil
	default:
		return nil, nil, ErrInvalidInput
	}
}

func decodePeoplePayload[T any](payload []byte) (T, error) {
	var result T
	if len(payload) == 0 || len(payload) > MaximumPayloadBytes {
		return result, ErrInvalidInput
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&result); err != nil {
		return result, ErrInvalidInput
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return result, ErrInvalidInput
	}
	if !canonicalJSONEqual(payload, result) {
		return result, ErrInvalidInput
	}
	return result, nil
}

func canonicalPeopleText(value string) bool   { return value == strings.TrimSpace(value) }
func canonicalPeopleStatus(value string) bool { return value == "active" || value == "inactive" }

func validPeopleRecordID(value string) bool {
	if len(value) != 32 {
		return false
	}
	for _, character := range value {
		if character < '0' || character > '9' && character < 'a' || character > 'f' {
			return false
		}
	}
	return true
}

func validPeopleStableID(value string) bool {
	if len(value) < 1 || len(value) > 128 {
		return false
	}
	for index, character := range value {
		if character >= 'A' && character <= 'Z' || character >= 'a' && character <= 'z' || character >= '0' && character <= '9' || index > 0 && strings.ContainsRune("._:-", character) {
			continue
		}
		return false
	}
	return true
}

func canonicalPeopleSitePayload(value peopleSitePayload) bool {
	return canonicalPeopleText(value.Name) && value.Name != "" && canonicalPeopleText(value.Address1) && canonicalPeopleText(value.Address2) &&
		canonicalPeopleText(value.City) && canonicalPeopleText(value.Region) && canonicalPeopleText(value.PostalCode) && value.Country == strings.ToUpper(strings.TrimSpace(value.Country)) && canonicalPeopleStatus(value.Status)
}
func canonicalPeopleBuildingPayload(value peopleBuildingPayload) bool {
	return canonicalPeopleText(value.SiteID) && value.SiteID != "" && canonicalPeopleText(value.Name) && value.Name != "" && canonicalPeopleStatus(value.Status)
}
func canonicalPeopleRoomPayload(value peopleRoomPayload) bool {
	return canonicalPeopleText(value.SiteID) && value.SiteID != "" && canonicalPeopleText(value.BuildingID) && value.BuildingID != "" && canonicalPeopleText(value.Number) && value.Number != "" && canonicalPeopleText(value.Name) && canonicalPeopleStatus(value.Status)
}
func canonicalPeopleDepartmentPayload(value peopleDepartmentPayload) bool {
	return canonicalPeopleText(value.Name) && value.Name != "" && canonicalPeopleText(value.SiteID) && canonicalPeopleStatus(value.Status)
}
func canonicalPeopleIdentityPayload(value peopleIdentityPayload) bool {
	validKind := value.Kind == "person" || value.Kind == "shared" || value.Kind == "public" || value.Kind == "lab"
	return validKind && canonicalPeopleText(value.DisplayName) && value.DisplayName != "" && value.Email == strings.ToLower(strings.TrimSpace(value.Email)) &&
		(value.Kind != "person" || value.Email != "") &&
		canonicalPeopleText(value.DepartmentID) && canonicalPeopleText(value.SiteID) && canonicalPeopleStatus(value.Status) &&
		value.Provider == strings.ToLower(strings.TrimSpace(value.Provider)) && canonicalPeopleText(value.ProviderSubject) && (value.Provider == "") == (value.ProviderSubject == "")
}
func canonicalPeopleAssignmentPayload(value peopleAssignmentPayload) bool {
	validRole := value.AssigneeKind == "identity" && (value.Role == "primary" || value.Role == "user") ||
		value.AssigneeKind == "department" && value.Role == "department"
	return canonicalPeopleText(value.AssetID) && value.AssetID != "" && (value.AssigneeKind == "identity" || value.AssigneeKind == "department") &&
		canonicalPeopleText(value.AssigneeID) && value.AssigneeID != "" && validRole
}

func peopleInstant(value time.Time) string { return value.Format(time.RFC3339Nano) }
func peopleOptionalInstant(value *time.Time) string {
	if value == nil {
		return ""
	}
	return peopleInstant(*value)
}
func parsePeopleInstant(value string) (time.Time, error) {
	return parsePortableInstant(value, 2000)
}
func parsePeopleOptionalInstant(value string) (*time.Time, error) {
	if value == "" {
		return nil, nil
	}
	parsed, err := parsePeopleInstant(value)
	if err != nil {
		return nil, err
	}
	return &parsed, nil
}
func parsePeopleStateTimes(created, updated string) (time.Time, time.Time, error) {
	createdAt, err := parsePeopleInstant(created)
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	updatedAt, err := parsePeopleInstant(updated)
	if err != nil || updatedAt.Before(createdAt) {
		return time.Time{}, time.Time{}, ErrInvalidInput
	}
	return createdAt, updatedAt, nil
}

func exactPeopleRecord[T any](current, candidate T, err error, same func(T, T) bool) (bool, error) {
	if errors.Is(err, people.ErrNotFound) {
		return false, nil
	}
	return err == nil && same(current, candidate), err
}
func samePeopleSite(left, right people.Site) bool {
	right.OrganizationID = left.OrganizationID
	return left == right
}
func samePeopleBuilding(left, right people.Building) bool {
	right.OrganizationID = left.OrganizationID
	return left == right
}
func samePeopleRoom(left, right people.Room) bool {
	right.OrganizationID = left.OrganizationID
	return left == right
}
func samePeopleDepartment(left, right people.Department) bool {
	right.OrganizationID = left.OrganizationID
	return left == right
}
func samePeopleIdentity(left, right people.Identity) bool {
	right.OrganizationID = left.OrganizationID
	return left == right
}
func samePeopleAssignment(left, right people.AssetAssignment) bool {
	right.OrganizationID = left.OrganizationID
	return left.ID == right.ID && left.OrganizationID == right.OrganizationID && left.AssetID == right.AssetID && left.AssigneeKind == right.AssigneeKind &&
		left.AssigneeID == right.AssigneeID && left.Role == right.Role && left.EffectiveFrom.Equal(right.EffectiveFrom) &&
		(left.EffectiveTo == nil && right.EffectiveTo == nil || left.EffectiveTo != nil && right.EffectiveTo != nil && left.EffectiveTo.Equal(*right.EffectiveTo)) &&
		left.CreatedAt.Equal(right.CreatedAt)
}
