package exchange

// Requirements: REQ-EXCHANGE-001, REQ-LEDGER-001, REQ-PATTERNS-001. Features: migration.packages, procurement.finance, templates.schemas. GitHub: #9.

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

	"github.com/maxlemke/stewardmesh/internal/ledger"
)

var ledgerRecordTypes = []string{"ledger.vendor", "ledger.purchase-order", "ledger.contract", "ledger.commitment", "ledger.budget", "ledger.cost"}

type LedgerProvider struct {
	service  *ledger.Service
	importer ledger.ExchangeImporter
}

type ledgerStatePayload struct {
	CreatedAt string `json:"createdAt"`
	UpdatedAt string `json:"updatedAt"`
}

type ledgerVendorPayload struct {
	Name       string `json:"name"`
	ExternalID string `json:"externalId,omitempty"`
	Status     string `json:"status"`
	ledgerStatePayload
}

type ledgerPurchaseOrderPayload struct {
	Number             string `json:"number"`
	VendorID           string `json:"vendorId"`
	Status             string `json:"status"`
	Currency           string `json:"currency"`
	TotalMinor         int64  `json:"totalMinor"`
	OrderedOn          string `json:"orderedOn,omitempty"`
	AssetIDs           string `json:"assetIds"`
	ReceiptDocumentIDs string `json:"receiptDocumentIds"`
	ledgerStatePayload
}

type ledgerContractPayload struct {
	Name              string `json:"name"`
	VendorID          string `json:"vendorId"`
	OperationalStatus string `json:"operationalStatus"`
	FinancialStatus   string `json:"financialStatus"`
	Currency          string `json:"currency"`
	CeilingMinor      int64  `json:"ceilingMinor"`
	StartsOn          string `json:"startsOn"`
	EndsOn            string `json:"endsOn"`
	RenewsOn          string `json:"renewsOn,omitempty"`
	DocumentIDs       string `json:"documentIds"`
	ledgerStatePayload
}

type ledgerCommitmentPayload struct {
	ContractID   string `json:"contractId"`
	Kind         string `json:"kind"`
	Description  string `json:"description"`
	Currency     string `json:"currency"`
	AmountMinor  int64  `json:"amountMinor"`
	StartsOn     string `json:"startsOn"`
	EndsOn       string `json:"endsOn"`
	FiscalPeriod string `json:"fiscalPeriod"`
	Scenario     string `json:"scenario"`
	ledgerStatePayload
}

type ledgerBudgetPayload struct {
	Name           string `json:"name"`
	FiscalPeriod   string `json:"fiscalPeriod"`
	Scenario       string `json:"scenario"`
	DepartmentID   string `json:"departmentId,omitempty"`
	SiteID         string `json:"siteId,omitempty"`
	Currency       string `json:"currency"`
	AllocatedMinor int64  `json:"allocatedMinor"`
	ledgerStatePayload
}

type ledgerCostPayload struct {
	Description       string `json:"description"`
	Kind              string `json:"kind"`
	Currency          string `json:"currency"`
	AmountMinor       int64  `json:"amountMinor"`
	FiscalPeriod      string `json:"fiscalPeriod"`
	Scenario          string `json:"scenario"`
	PurchaseOrderID   string `json:"purchaseOrderId,omitempty"`
	ContractID        string `json:"contractId,omitempty"`
	AssetID           string `json:"assetId,omitempty"`
	DepartmentID      string `json:"departmentId,omitempty"`
	SiteID            string `json:"siteId,omitempty"`
	DocumentID        string `json:"documentId,omitempty"`
	ExternalReference string `json:"externalReference,omitempty"`
	SourceSystemID    string `json:"sourceSystemId,omitempty"`
	SourceRecordID    string `json:"sourceRecordId,omitempty"`
	ledgerStatePayload
}

func NewLedgerProvider(service *ledger.Service, importer ledger.ExchangeImporter) (*LedgerProvider, error) {
	if service == nil || importer == nil || !service.OwnsExchangeImporter(importer) {
		return nil, errors.New("Ledger service and its construction-time Exchange importer are required")
	}
	return &LedgerProvider{service: service, importer: importer}, nil
}

func (*LedgerProvider) Types() []string { return append([]string(nil), ledgerRecordTypes...) }

func (p *LedgerProvider) ListRecords(ctx context.Context) ([]Record, error) {
	snapshot, err := p.service.ExchangeSnapshot(ctx, MaximumRecords)
	if errors.Is(err, ledger.ErrTooLarge) {
		return nil, ErrTooLarge
	}
	if err != nil {
		return nil, err
	}
	result := make([]Record, 0, len(snapshot.Vendors)+len(snapshot.PurchaseOrders)+len(snapshot.Contracts)+len(snapshot.Commitments)+len(snapshot.Budgets)+len(snapshot.Costs))
	appendRecord := func(recordType, id string, revision int64, dependencies []Reference, provenance Provenance, payload any) error {
		encoded, err := marshalLedgerPayload(payload)
		if err != nil {
			return err
		}
		result = append(result, Record{Type: recordType, ID: id, Revision: revision, Dependencies: normalizeReferences(dependencies), Provenance: provenance, Ownership: OwnershipMetadata{State: "local"}, Payload: encoded})
		return nil
	}
	for _, item := range snapshot.Vendors {
		if err := validatePortableInstants(2000, item.CreatedAt, item.UpdatedAt); err != nil {
			return nil, err
		}
		if err := appendRecord("ledger.vendor", item.ID, item.Revision, nil, Provenance{}, ledgerVendorPayload{Name: item.Name, ExternalID: item.ExternalID, Status: item.Status, ledgerStatePayload: ledgerState(item.CreatedAt, item.UpdatedAt)}); err != nil {
			return nil, err
		}
	}
	for _, item := range snapshot.PurchaseOrders {
		if err := validatePortableInstants(2000, item.CreatedAt, item.UpdatedAt); err != nil {
			return nil, err
		}
		assetIDs, err := encodeLedgerIDs(item.AssetIDs)
		if err != nil {
			return nil, err
		}
		documentIDs, err := encodeLedgerIDs(item.ReceiptDocumentIDs)
		if err != nil {
			return nil, err
		}
		if err := appendRecord("ledger.purchase-order", item.ID, item.Revision, ledgerPurchaseOrderDependencies(item), Provenance{}, ledgerPurchaseOrderPayload{
			Number: item.Number, VendorID: item.VendorID, Status: item.Status, Currency: item.Currency, TotalMinor: item.TotalMinor,
			OrderedOn: ledgerOptionalDate(item.OrderedOn), AssetIDs: assetIDs, ReceiptDocumentIDs: documentIDs, ledgerStatePayload: ledgerState(item.CreatedAt, item.UpdatedAt),
		}); err != nil {
			return nil, err
		}
	}
	for _, item := range snapshot.Contracts {
		if err := validatePortableInstants(2000, item.CreatedAt, item.UpdatedAt); err != nil {
			return nil, err
		}
		documentIDs, err := encodeLedgerIDs(item.DocumentIDs)
		if err != nil {
			return nil, err
		}
		if err := appendRecord("ledger.contract", item.ID, item.Revision, ledgerContractDependencies(item), Provenance{}, ledgerContractPayload{
			Name: item.Name, VendorID: item.VendorID, OperationalStatus: item.OperationalStatus, FinancialStatus: item.FinancialStatus,
			Currency: item.Currency, CeilingMinor: item.CeilingMinor, StartsOn: ledgerDate(item.StartsOn), EndsOn: ledgerDate(item.EndsOn),
			RenewsOn: ledgerOptionalDate(item.RenewsOn), DocumentIDs: documentIDs, ledgerStatePayload: ledgerState(item.CreatedAt, item.UpdatedAt),
		}); err != nil {
			return nil, err
		}
	}
	for _, item := range snapshot.Commitments {
		if err := validatePortableInstants(2000, item.CreatedAt, item.UpdatedAt); err != nil {
			return nil, err
		}
		if err := appendRecord("ledger.commitment", item.ID, item.Revision, []Reference{{Type: "ledger.contract", ID: item.ContractID}}, Provenance{}, ledgerCommitmentPayload{
			ContractID: item.ContractID, Kind: item.Kind, Description: item.Description, Currency: item.Currency, AmountMinor: item.AmountMinor,
			StartsOn: ledgerDate(item.StartsOn), EndsOn: ledgerDate(item.EndsOn), FiscalPeriod: item.FiscalPeriod, Scenario: item.Scenario,
			ledgerStatePayload: ledgerState(item.CreatedAt, item.UpdatedAt),
		}); err != nil {
			return nil, err
		}
	}
	for _, item := range snapshot.Budgets {
		if err := validatePortableInstants(2000, item.CreatedAt, item.UpdatedAt); err != nil {
			return nil, err
		}
		if err := appendRecord("ledger.budget", item.ID, item.Revision, ledgerBudgetDependencies(item), Provenance{}, ledgerBudgetPayload{
			Name: item.Name, FiscalPeriod: item.FiscalPeriod, Scenario: item.Scenario, DepartmentID: item.DepartmentID,
			SiteID: item.SiteID, Currency: item.Currency, AllocatedMinor: item.AllocatedMinor, ledgerStatePayload: ledgerState(item.CreatedAt, item.UpdatedAt),
		}); err != nil {
			return nil, err
		}
	}
	for _, item := range snapshot.Costs {
		if err := validatePortableInstants(2000, item.CreatedAt, item.UpdatedAt); err != nil {
			return nil, err
		}
		provenance := Provenance{}
		if stableIDPattern.MatchString(item.SourceSystemID) && safeSourceRecordID(item.SourceRecordID) {
			provenance = Provenance{SourceSystemID: item.SourceSystemID, SourceRecordID: item.SourceRecordID}
		}
		if err := appendRecord("ledger.cost", item.ID, item.Revision, ledgerCostDependencies(item), provenance, ledgerCostPayload{
			Description: item.Description, Kind: item.Kind, Currency: item.Currency, AmountMinor: item.AmountMinor,
			FiscalPeriod: item.FiscalPeriod, Scenario: item.Scenario, PurchaseOrderID: item.PurchaseOrderID, ContractID: item.ContractID,
			AssetID: item.AssetID, DepartmentID: item.DepartmentID, SiteID: item.SiteID, DocumentID: item.DocumentID,
			ExternalReference: item.ExternalReference, SourceSystemID: item.SourceSystemID, SourceRecordID: item.SourceRecordID,
			ledgerStatePayload: ledgerState(item.CreatedAt, item.UpdatedAt),
		}); err != nil {
			return nil, err
		}
	}
	sort.Slice(result, func(i, j int) bool {
		return (Reference{Type: result[i].Type, ID: result[i].ID}).Key() < (Reference{Type: result[j].Type, ID: result[j].ID}).Key()
	})
	return result, nil
}

func (p *LedgerProvider) Exists(ctx context.Context, reference Reference) (bool, error) {
	var err error
	switch reference.Type {
	case "ledger.vendor":
		_, err = p.service.GetVendor(ctx, reference.ID)
	case "ledger.purchase-order":
		_, err = p.service.GetPurchaseOrder(ctx, reference.ID)
	case "ledger.contract":
		_, err = p.service.GetContract(ctx, reference.ID)
	case "ledger.commitment":
		_, err = p.service.GetCommitment(ctx, reference.ID)
	case "ledger.budget":
		_, err = p.service.GetBudget(ctx, reference.ID)
	case "ledger.cost":
		_, err = p.service.GetCost(ctx, reference.ID)
	default:
		return false, nil
	}
	if errors.Is(err, ledger.ErrNotFound) {
		return false, nil
	}
	return err == nil, err
}

func (p *LedgerProvider) ImportRecordExists(ctx context.Context, record Record, _ []byte) (bool, error) {
	_, dependencies, err := decodeLedgerRecord(record)
	if err != nil || !slices.Equal(dependencies, record.Dependencies) {
		return false, ErrInvalidInput
	}
	records, err := p.ListRecords(ctx)
	if err != nil {
		return false, err
	}
	for _, current := range records {
		if current.Type == record.Type && current.ID == record.ID {
			return current.Revision == record.Revision && slices.Equal(current.Dependencies, record.Dependencies) && bytes.Equal(current.Payload, record.Payload), nil
		}
	}
	return false, nil
}

func (p *LedgerProvider) ImportRecord(ctx context.Context, operation ProviderImportOperation, _ string, record Record, _ []byte) (ProviderImportResult, error) {
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
	candidate, dependencies, err := decodeLedgerRecord(record)
	if err != nil || !slices.Equal(dependencies, record.Dependencies) {
		return ProviderImportResult{}, ErrInvalidInput
	}
	domainOperation := ledger.ExchangeImportOperation{Token: operation.Token, OccurredAt: operation.OccurredAt}
	var result ledger.ExchangeImportResult
	switch value := candidate.(type) {
	case ledger.Vendor:
		result, err = p.importer.ImportVendor(ctx, domainOperation, value)
	case ledger.PurchaseOrder:
		result, err = p.importer.ImportPurchaseOrder(ctx, domainOperation, value)
	case ledger.Contract:
		result, err = p.importer.ImportContract(ctx, domainOperation, value)
	case ledger.Commitment:
		result, err = p.importer.ImportCommitment(ctx, domainOperation, value)
	case ledger.Budget:
		result, err = p.importer.ImportBudget(ctx, domainOperation, value)
	case ledger.CostRecord:
		result, err = p.importer.ImportCost(ctx, domainOperation, value)
	default:
		return ProviderImportResult{}, ErrInvalidInput
	}
	providerResult := ProviderImportResult{Committed: result.Committed, Created: result.Created}
	switch {
	case errors.Is(err, ledger.ErrInvalidInput):
		return providerResult, ErrInvalidInput
	case errors.Is(err, ledger.ErrConflict), errors.Is(err, ledger.ErrInvalidTransition):
		return providerResult, ErrConflict
	case errors.Is(err, ledger.ErrNotFound), errors.Is(err, ledger.ErrReferenceMissing):
		return providerResult, ErrDependencyMissing
	default:
		return providerResult, err
	}
}

func decodeLedgerRecord(record Record) (any, []Reference, error) {
	if record.Revision < 1 || !stableIDPattern.MatchString(record.ID) {
		return nil, nil, ErrInvalidInput
	}
	switch record.Type {
	case "ledger.vendor":
		payload, err := decodeLedgerPayload[ledgerVendorPayload](record.Payload)
		createdAt, updatedAt, timeErr := parseLedgerState(payload.ledgerStatePayload)
		if err != nil || timeErr != nil || !canonicalLedgerText(payload.Name) || payload.Name == "" || !canonicalLedgerText(payload.ExternalID) || !canonicalLedgerLower(payload.Status) {
			return nil, nil, ErrInvalidInput
		}
		return ledger.Vendor{ID: record.ID, Name: payload.Name, ExternalID: payload.ExternalID, Status: payload.Status, Revision: record.Revision, CreatedAt: createdAt, UpdatedAt: updatedAt}, []Reference{}, nil
	case "ledger.purchase-order":
		payload, err := decodeLedgerPayload[ledgerPurchaseOrderPayload](record.Payload)
		createdAt, updatedAt, timeErr := parseLedgerState(payload.ledgerStatePayload)
		orderedOn, dateErr := parseLedgerOptionalDate(payload.OrderedOn)
		assetIDs, assetErr := decodeLedgerIDs(payload.AssetIDs)
		documentIDs, documentErr := decodeLedgerIDs(payload.ReceiptDocumentIDs)
		value := ledger.PurchaseOrder{ID: record.ID, Number: payload.Number, VendorID: payload.VendorID, Status: payload.Status, Currency: payload.Currency,
			TotalMinor: payload.TotalMinor, OrderedOn: orderedOn, AssetIDs: assetIDs, ReceiptDocumentIDs: documentIDs,
			Revision: record.Revision, CreatedAt: createdAt, UpdatedAt: updatedAt}
		if err != nil || timeErr != nil || dateErr != nil || assetErr != nil || documentErr != nil || !canonicalLedgerText(payload.Number) || !canonicalLedgerText(payload.VendorID) ||
			!canonicalLedgerLower(payload.Status) || !canonicalLedgerUpper(payload.Currency) {
			return nil, nil, ErrInvalidInput
		}
		return value, ledgerPurchaseOrderDependencies(value), nil
	case "ledger.contract":
		payload, err := decodeLedgerPayload[ledgerContractPayload](record.Payload)
		createdAt, updatedAt, timeErr := parseLedgerState(payload.ledgerStatePayload)
		startsOn, startsErr := parseLedgerDate(payload.StartsOn)
		endsOn, endsErr := parseLedgerDate(payload.EndsOn)
		renewsOn, renewsErr := parseLedgerOptionalDate(payload.RenewsOn)
		documentIDs, documentErr := decodeLedgerIDs(payload.DocumentIDs)
		value := ledger.Contract{ID: record.ID, Name: payload.Name, VendorID: payload.VendorID, OperationalStatus: payload.OperationalStatus,
			FinancialStatus: payload.FinancialStatus, Currency: payload.Currency, CeilingMinor: payload.CeilingMinor, StartsOn: startsOn,
			EndsOn: endsOn, RenewsOn: renewsOn, DocumentIDs: documentIDs, Revision: record.Revision, CreatedAt: createdAt, UpdatedAt: updatedAt}
		if err != nil || timeErr != nil || startsErr != nil || endsErr != nil || renewsErr != nil || documentErr != nil || !canonicalLedgerText(payload.Name) ||
			!canonicalLedgerText(payload.VendorID) || !canonicalLedgerLower(payload.OperationalStatus) || !canonicalLedgerLower(payload.FinancialStatus) ||
			!canonicalLedgerUpper(payload.Currency) {
			return nil, nil, ErrInvalidInput
		}
		return value, ledgerContractDependencies(value), nil
	case "ledger.commitment":
		payload, err := decodeLedgerPayload[ledgerCommitmentPayload](record.Payload)
		createdAt, updatedAt, timeErr := parseLedgerState(payload.ledgerStatePayload)
		startsOn, startsErr := parseLedgerDate(payload.StartsOn)
		endsOn, endsErr := parseLedgerDate(payload.EndsOn)
		value := ledger.Commitment{ID: record.ID, ContractID: payload.ContractID, Kind: payload.Kind, Description: payload.Description,
			Currency: payload.Currency, AmountMinor: payload.AmountMinor, StartsOn: startsOn, EndsOn: endsOn, FiscalPeriod: payload.FiscalPeriod,
			Scenario: payload.Scenario, Revision: record.Revision, CreatedAt: createdAt, UpdatedAt: updatedAt}
		if err != nil || timeErr != nil || startsErr != nil || endsErr != nil || !canonicalLedgerText(payload.ContractID) ||
			!canonicalLedgerLower(payload.Kind) || !canonicalLedgerText(payload.Description) || !canonicalLedgerUpper(payload.Currency) ||
			!canonicalLedgerText(payload.FiscalPeriod) || !canonicalLedgerLower(payload.Scenario) {
			return nil, nil, ErrInvalidInput
		}
		return value, []Reference{{Type: "ledger.contract", ID: value.ContractID}}, nil
	case "ledger.budget":
		payload, err := decodeLedgerPayload[ledgerBudgetPayload](record.Payload)
		createdAt, updatedAt, timeErr := parseLedgerState(payload.ledgerStatePayload)
		value := ledger.Budget{ID: record.ID, Name: payload.Name, FiscalPeriod: payload.FiscalPeriod, Scenario: payload.Scenario,
			DepartmentID: payload.DepartmentID, SiteID: payload.SiteID, Currency: payload.Currency, AllocatedMinor: payload.AllocatedMinor,
			Revision: record.Revision, CreatedAt: createdAt, UpdatedAt: updatedAt}
		if err != nil || timeErr != nil || !canonicalLedgerText(payload.Name) || !canonicalLedgerText(payload.FiscalPeriod) ||
			!canonicalLedgerLower(payload.Scenario) || !canonicalLedgerText(payload.DepartmentID) || !canonicalLedgerText(payload.SiteID) || !canonicalLedgerUpper(payload.Currency) {
			return nil, nil, ErrInvalidInput
		}
		return value, ledgerBudgetDependencies(value), nil
	case "ledger.cost":
		payload, err := decodeLedgerPayload[ledgerCostPayload](record.Payload)
		createdAt, updatedAt, timeErr := parseLedgerState(payload.ledgerStatePayload)
		value := ledger.CostRecord{ID: record.ID, Description: payload.Description, Kind: payload.Kind, Currency: payload.Currency,
			AmountMinor: payload.AmountMinor, FiscalPeriod: payload.FiscalPeriod, Scenario: payload.Scenario, PurchaseOrderID: payload.PurchaseOrderID,
			ContractID: payload.ContractID, AssetID: payload.AssetID, DepartmentID: payload.DepartmentID, SiteID: payload.SiteID,
			DocumentID: payload.DocumentID, ExternalReference: payload.ExternalReference, SourceSystemID: payload.SourceSystemID,
			SourceRecordID: payload.SourceRecordID, Revision: record.Revision, CreatedAt: createdAt, UpdatedAt: updatedAt}
		if err != nil || timeErr != nil || !canonicalLedgerText(payload.Description) || !canonicalLedgerLower(payload.Kind) ||
			!canonicalLedgerUpper(payload.Currency) || !canonicalLedgerText(payload.FiscalPeriod) || !canonicalLedgerLower(payload.Scenario) ||
			!canonicalLedgerText(payload.PurchaseOrderID) || !canonicalLedgerText(payload.ContractID) || !canonicalLedgerText(payload.AssetID) ||
			!canonicalLedgerText(payload.DepartmentID) || !canonicalLedgerText(payload.SiteID) || !canonicalLedgerText(payload.DocumentID) ||
			!canonicalLedgerText(payload.ExternalReference) || !canonicalLedgerLower(payload.SourceSystemID) || !canonicalLedgerText(payload.SourceRecordID) {
			return nil, nil, ErrInvalidInput
		}
		return value, ledgerCostDependencies(value), nil
	default:
		return nil, nil, ErrInvalidInput
	}
}

func decodeLedgerPayload[T any](payload []byte) (T, error) {
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

func marshalLedgerPayload(value any) ([]byte, error) {
	payload, err := json.Marshal(value)
	if err != nil || len(payload) == 0 || len(payload) > MaximumPayloadBytes {
		return nil, ErrInvalidInput
	}
	return payload, nil
}

func ledgerPurchaseOrderDependencies(value ledger.PurchaseOrder) []Reference {
	result := []Reference{{Type: "ledger.vendor", ID: value.VendorID}}
	for _, id := range value.AssetIDs {
		result = append(result, Reference{Type: "atlas.asset", ID: id})
	}
	for _, id := range value.ReceiptDocumentIDs {
		result = append(result, Reference{Type: "vault.blob", ID: id})
	}
	return normalizeReferences(result)
}

func ledgerContractDependencies(value ledger.Contract) []Reference {
	result := []Reference{{Type: "ledger.vendor", ID: value.VendorID}}
	for _, id := range value.DocumentIDs {
		result = append(result, Reference{Type: "vault.blob", ID: id})
	}
	return normalizeReferences(result)
}

func ledgerBudgetDependencies(value ledger.Budget) []Reference {
	result := []Reference{}
	if value.DepartmentID != "" {
		result = append(result, Reference{Type: "people.department", ID: value.DepartmentID})
	}
	if value.SiteID != "" {
		result = append(result, Reference{Type: "people.site", ID: value.SiteID})
	}
	return normalizeReferences(result)
}

func ledgerCostDependencies(value ledger.CostRecord) []Reference {
	result := []Reference{}
	for _, item := range []Reference{
		{Type: "ledger.purchase-order", ID: value.PurchaseOrderID}, {Type: "ledger.contract", ID: value.ContractID},
		{Type: "atlas.asset", ID: value.AssetID}, {Type: "people.department", ID: value.DepartmentID},
		{Type: "people.site", ID: value.SiteID}, {Type: "vault.blob", ID: value.DocumentID},
	} {
		if item.ID != "" {
			result = append(result, item)
		}
	}
	return normalizeReferences(result)
}

func ledgerState(createdAt, updatedAt time.Time) ledgerStatePayload {
	return ledgerStatePayload{CreatedAt: ledgerInstant(createdAt), UpdatedAt: ledgerInstant(updatedAt)}
}

func canonicalLedgerText(value string) bool { return value == strings.TrimSpace(value) }
func canonicalLedgerLower(value string) bool {
	return value == strings.ToLower(strings.TrimSpace(value))
}
func canonicalLedgerUpper(value string) bool {
	return value == strings.ToUpper(strings.TrimSpace(value))
}

func encodeLedgerIDs(values []string) (string, error) {
	if values == nil {
		values = []string{}
	}
	if !sort.StringsAreSorted(values) || hasDuplicateLedgerIDs(values) {
		return "", ErrInvalidInput
	}
	encoded, err := json.Marshal(values)
	if err != nil || len(encoded) > 40_000 {
		return "", ErrInvalidInput
	}
	return string(encoded), nil
}

func decodeLedgerIDs(value string) ([]string, error) {
	var result []string
	if err := json.Unmarshal([]byte(value), &result); err != nil || result == nil {
		return nil, ErrInvalidInput
	}
	canonical, err := encodeLedgerIDs(result)
	if err != nil || canonical != value {
		return nil, ErrInvalidInput
	}
	return result, nil
}

func hasDuplicateLedgerIDs(values []string) bool {
	for index := 1; index < len(values); index++ {
		if values[index] == values[index-1] {
			return true
		}
	}
	return false
}

func ledgerInstant(value time.Time) string { return value.Format(time.RFC3339Nano) }
func ledgerDate(value time.Time) string    { return value.UTC().Format("2006-01-02") }

func ledgerOptionalDate(value *time.Time) string {
	if value == nil {
		return ""
	}
	return ledgerDate(*value)
}

func parseLedgerInstant(value string) (time.Time, error) {
	return parsePortableInstant(value, 2000)
}

func parseLedgerState(value ledgerStatePayload) (time.Time, time.Time, error) {
	createdAt, err := parseLedgerInstant(value.CreatedAt)
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	updatedAt, err := parseLedgerInstant(value.UpdatedAt)
	if err != nil || updatedAt.Before(createdAt) {
		return time.Time{}, time.Time{}, ErrInvalidInput
	}
	return createdAt, updatedAt, nil
}

func parseLedgerDate(value string) (time.Time, error) {
	parsed, err := time.Parse("2006-01-02", value)
	if err != nil || ledgerDate(parsed) != value || parsed.Year() < 1970 || parsed.Year() > 9999 {
		return time.Time{}, ErrInvalidInput
	}
	return parsed, nil
}

func parseLedgerOptionalDate(value string) (*time.Time, error) {
	if value == "" {
		return nil, nil
	}
	parsed, err := parseLedgerDate(value)
	if err != nil {
		return nil, err
	}
	return &parsed, nil
}
