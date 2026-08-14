package patterns

// Requirement: REQ-PATTERNS-001. Feature: templates.schemas. GitHub: #8.

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/maxlemke/stewardmesh/internal/foundation"
)

var (
	stableIDPattern   = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)
	fieldKeyPattern   = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_.-]{0,63}$`)
	recordTypePattern = regexp.MustCompile(`^[a-z][a-z0-9.-]{1,79}$`)
	currencyPattern   = regexp.MustCompile(`^[A-Z]{3}$`)
)

type ServiceConfig struct {
	OrganizationID string
	Now            func() time.Time
}

type Service struct {
	store          Store
	writes         WriteGate
	auditor        foundation.Auditor
	organizationID string
	now            func() time.Time
	builtIns       map[string]map[int64]Template
}

type exchangeImporter struct{ service *Service }

type exchangeImportContextKey struct{}

type exchangeImportContext struct{ operation ExchangeImportOperation }

func NewService(store Store, auditor foundation.Auditor, configuration ServiceConfig) (*Service, error) {
	service, _, err := NewServiceWithExchangeImporter(store, nil, auditor, configuration)
	return service, err
}

func NewServiceWithExchangeImporter(store Store, writes WriteGate, auditor foundation.Auditor, configuration ServiceConfig) (*Service, ExchangeImporter, error) {
	if store == nil || auditor == nil {
		return nil, nil, fmt.Errorf("Patterns store and auditor are required")
	}
	configuration.OrganizationID = strings.TrimSpace(configuration.OrganizationID)
	if configuration.OrganizationID == "" {
		return nil, nil, fmt.Errorf("Patterns organization id is required")
	}
	if configuration.Now == nil {
		configuration.Now = func() time.Time { return time.Now().UTC() }
	}
	builtIns := make(map[string]map[int64]Template)
	for _, candidate := range BuiltInTemplates() {
		fields, err := normalizeFields(candidate.Fields)
		if err != nil {
			return nil, nil, fmt.Errorf("invalid built-in Patterns template %q: %w", candidate.ID, err)
		}
		candidate.Fields = fields
		candidate.CreatedAt = time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
		if candidate.Version < 1 || !candidate.BuiltIn || candidate.Status == "" || !stableIDPattern.MatchString(candidate.ID) || !recordTypePattern.MatchString(candidate.RecordType) {
			return nil, nil, fmt.Errorf("invalid built-in Patterns template metadata %q", candidate.ID)
		}
		if builtIns[candidate.ID] == nil {
			builtIns[candidate.ID] = make(map[int64]Template)
		}
		if _, duplicate := builtIns[candidate.ID][candidate.Version]; duplicate {
			return nil, nil, fmt.Errorf("duplicate built-in Patterns template %q version %d", candidate.ID, candidate.Version)
		}
		builtIns[candidate.ID][candidate.Version] = candidate
	}
	service := &Service{store: store, writes: writes, auditor: auditor, organizationID: configuration.OrganizationID, now: configuration.Now, builtIns: builtIns}
	return service, &exchangeImporter{service: service}, nil
}

func (*exchangeImporter) patternsExchangeImporter() {}

func (s *Service) OwnsExchangeImporter(candidate ExchangeImporter) bool {
	importer, ok := candidate.(*exchangeImporter)
	return ok && importer != nil && importer.service == s
}

// ExchangeTemplates returns every custom template with its complete immutable
// history. Local operator/timestamp fields are deliberately projected away.
func (s *Service) ExchangeTemplates(ctx context.Context) ([]ExchangeTemplate, error) {
	items, err := s.store.ListTemplates(ctx, s.organizationID, ListQuery{IncludeRetired: true, IncludeVersions: true})
	if err != nil {
		return nil, err
	}
	grouped := make(map[string]*ExchangeTemplate)
	for _, item := range items {
		candidate := grouped[item.ID]
		if candidate == nil {
			candidate = &ExchangeTemplate{ID: item.ID, RecordType: item.RecordType, Name: item.Name, Versions: []ExchangeTemplateVersion{}}
			grouped[item.ID] = candidate
		}
		candidate.Versions = append(candidate.Versions, ExchangeTemplateVersion{
			Description: item.Description, Version: item.Version, Status: item.Status, Fields: cloneFields(item.Fields),
		})
	}
	result := make([]ExchangeTemplate, 0, len(grouped))
	for _, item := range grouped {
		sort.Slice(item.Versions, func(i, j int) bool { return item.Versions[i].Version < item.Versions[j].Version })
		normalized, err := normalizeExchangeTemplate(*item)
		if err != nil {
			return nil, err
		}
		result = append(result, normalized)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result, nil
}

func (s *Service) ExchangeTemplate(ctx context.Context, id string) (ExchangeTemplate, error) {
	id = strings.TrimSpace(id)
	if !stableIDPattern.MatchString(id) {
		return ExchangeTemplate{}, ErrInvalidInput
	}
	items, err := s.ExchangeTemplates(ctx)
	if err != nil {
		return ExchangeTemplate{}, err
	}
	for _, item := range items {
		if item.ID == id {
			return item, nil
		}
	}
	return ExchangeTemplate{}, ErrNotFound
}

func (i *exchangeImporter) ImportTemplate(ctx context.Context, operation ExchangeImportOperation, candidate ExchangeTemplate) (ExchangeImportResult, error) {
	operation.Token = strings.TrimSpace(operation.Token)
	operation.OccurredAt = operation.OccurredAt.UTC()
	if !stableIDPattern.MatchString(operation.Token) || operation.OccurredAt.IsZero() || operation.OccurredAt.Year() < 2000 || operation.OccurredAt.Year() > 9999 {
		return ExchangeImportResult{}, ErrInvalidInput
	}
	normalized, err := normalizeExchangeTemplate(candidate)
	if err != nil || !sameExchangeTemplate(normalized, candidate) {
		return ExchangeImportResult{}, ErrInvalidInput
	}
	ctx = context.WithValue(ctx, exchangeImportContextKey{}, exchangeImportContext{operation: operation})
	existing, err := i.service.ExchangeTemplate(ctx, normalized.ID)
	if err == nil {
		if !sameExchangeTemplate(existing, normalized) {
			return ExchangeImportResult{}, ErrConflict
		}
		err = i.service.audit(ctx, "patterns.template.imported", exchangeAuditTemplate(normalized, i.service.organizationID, operation.OccurredAt), map[string]string{
			"versionCount": strconv.Itoa(len(normalized.Versions)),
		})
		return ExchangeImportResult{Committed: true}, err
	}
	if !errors.Is(err, ErrNotFound) {
		return ExchangeImportResult{}, err
	}
	history := exchangeHistory(normalized, i.service.organizationID, operation.OccurredAt)
	err = i.service.store.ImportTemplateHistory(ctx, i.service.organizationID, history)
	if err != nil {
		observed, readErr := i.service.ExchangeTemplate(ctx, normalized.ID)
		if readErr != nil || !sameExchangeTemplate(observed, normalized) {
			return ExchangeImportResult{}, err
		}
	}
	auditErr := i.service.audit(ctx, "patterns.template.imported", history[len(history)-1], map[string]string{
		"versionCount": strconv.Itoa(len(normalized.Versions)),
	})
	return ExchangeImportResult{Committed: true, Created: true}, errors.Join(err, auditErr)
}

func (s *Service) ListTemplates(ctx context.Context, query ListQuery) ([]Template, error) {
	query.RecordType = strings.ToLower(strings.TrimSpace(query.RecordType))
	if query.RecordType != "" && !recordTypePattern.MatchString(query.RecordType) {
		return nil, ErrInvalidInput
	}
	custom, err := s.store.ListTemplates(ctx, s.organizationID, query)
	if err != nil {
		return nil, err
	}
	items := make([]Template, 0, len(s.builtIns)+len(custom))
	for _, versions := range s.builtIns {
		candidates := []Template{latestBuiltIn(versions)}
		if query.IncludeVersions {
			candidates = candidates[:0]
			for _, candidate := range versions {
				candidates = append(candidates, candidate)
			}
		}
		for _, candidate := range candidates {
			if (query.RecordType == "" || candidate.RecordType == query.RecordType) && (query.IncludeRetired || candidate.Status != StatusRetired) {
				items = append(items, cloneTemplate(candidate))
			}
		}
	}
	for _, candidate := range custom {
		items = append(items, cloneTemplate(candidate))
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].RecordType != items[j].RecordType {
			return items[i].RecordType < items[j].RecordType
		}
		if items[i].BuiltIn != items[j].BuiltIn {
			return items[i].BuiltIn
		}
		if strings.ToLower(items[i].Name) != strings.ToLower(items[j].Name) {
			return strings.ToLower(items[i].Name) < strings.ToLower(items[j].Name)
		}
		if items[i].ID != items[j].ID {
			return items[i].ID < items[j].ID
		}
		return items[i].Version > items[j].Version
	})
	return items, nil
}

func (s *Service) GetTemplate(ctx context.Context, id string, version int64) (Template, error) {
	id = strings.TrimSpace(id)
	if !stableIDPattern.MatchString(id) || version < 0 {
		return Template{}, ErrInvalidInput
	}
	if versions, ok := s.builtIns[id]; ok {
		if version == 0 {
			return cloneTemplate(latestBuiltIn(versions)), nil
		}
		candidate, exists := versions[version]
		if !exists {
			return Template{}, ErrNotFound
		}
		return cloneTemplate(candidate), nil
	}
	candidate, err := s.store.GetTemplate(ctx, s.organizationID, id, version)
	if err != nil {
		return Template{}, err
	}
	return cloneTemplate(candidate), nil
}

// ActiveTemplateForRecordType resolves the deterministic schema that an
// automated consumer such as Exchange must pin. A built-in always wins over
// editable copies. Record families without a built-in are usable only when
// exactly one active custom template exists, preventing silent schema drift.
func (s *Service) ActiveTemplateForRecordType(ctx context.Context, recordType string) (Template, error) {
	recordType = strings.ToLower(strings.TrimSpace(recordType))
	if !recordTypePattern.MatchString(recordType) {
		return Template{}, ErrInvalidInput
	}
	if id, version, ok := BuiltInTemplateReference(recordType); ok {
		return s.GetTemplate(ctx, id, version)
	}
	items, err := s.ListTemplates(ctx, ListQuery{RecordType: recordType})
	if err != nil {
		return Template{}, err
	}
	var selected Template
	for _, item := range items {
		if item.Status != StatusActive {
			continue
		}
		if selected.ID != "" {
			return Template{}, ErrConflict
		}
		selected = item
	}
	if selected.ID == "" {
		return Template{}, ErrNotFound
	}
	return selected, nil
}

func latestBuiltIn(versions map[int64]Template) Template {
	var latest Template
	for version, candidate := range versions {
		if version > latest.Version {
			latest = candidate
		}
	}
	return latest
}

func (s *Service) CreateTemplate(ctx context.Context, input CreateTemplateInput) (Template, error) {
	id, err := templateID(input.ID)
	if err != nil {
		return Template{}, err
	}
	recordType := strings.ToLower(strings.TrimSpace(input.RecordType))
	name, description := strings.TrimSpace(input.Name), strings.TrimSpace(input.Description)
	fields, err := normalizeFields(input.Fields)
	if err != nil || !recordTypePattern.MatchString(recordType) || !validText(name, 1, 160) || !validText(description, 0, 1000) {
		return Template{}, ErrInvalidInput
	}
	if _, reserved := s.builtIns[id]; reserved {
		return Template{}, ErrConflict
	}
	if err := s.checkWrite(ctx, id); err != nil {
		return Template{}, err
	}
	now := s.now().UTC()
	created, err := s.store.CreateTemplate(ctx, Template{ID: id, OrganizationID: s.organizationID,
		RecordType: recordType, Name: name, Description: description, Version: 1, BuiltIn: false,
		Status: StatusActive, Fields: fields, CreatedBy: actorFromContext(ctx), CreatedAt: now})
	if err != nil {
		return Template{}, err
	}
	if err := s.audit(ctx, "patterns.template.created", created, nil); err != nil {
		return Template{}, fmt.Errorf("audit Patterns template creation: %w", err)
	}
	return cloneTemplate(created), nil
}

func (s *Service) CopyTemplate(ctx context.Context, sourceID string, sourceVersion int64, input CopyTemplateInput) (Template, error) {
	source, err := s.GetTemplate(ctx, sourceID, sourceVersion)
	if err != nil {
		return Template{}, err
	}
	description := strings.TrimSpace(input.Description)
	if description == "" {
		description = source.Description
	}
	return s.CreateTemplate(ctx, CreateTemplateInput{ID: input.ID, RecordType: source.RecordType,
		Name: input.Name, Description: description, Fields: cloneFields(source.Fields)})
}

func (s *Service) CreateVersion(ctx context.Context, id string, input NewVersionInput) (Template, error) {
	id = strings.TrimSpace(id)
	if !stableIDPattern.MatchString(id) {
		return Template{}, ErrInvalidInput
	}
	if err := s.checkWrite(ctx, id); err != nil {
		return Template{}, err
	}
	current, err := s.GetTemplate(ctx, id, 0)
	if err != nil {
		return Template{}, err
	}
	if current.BuiltIn {
		return Template{}, ErrConflict
	}
	fields, err := normalizeFields(input.Fields)
	description := strings.TrimSpace(input.Description)
	if err != nil || !validText(description, 0, 1000) {
		return Template{}, ErrInvalidInput
	}
	if description == "" {
		description = current.Description
	}
	next := current
	next.Description = description
	next.Fields = fields
	next.Version++
	next.CreatedBy = actorFromContext(ctx)
	next.CreatedAt = s.now().UTC()
	created, err := s.store.CreateVersion(ctx, next)
	if err != nil {
		return Template{}, err
	}
	if err := s.audit(ctx, "patterns.template.version.created", created, map[string]string{"previousVersion": strconv.FormatInt(current.Version, 10)}); err != nil {
		return Template{}, fmt.Errorf("audit Patterns template version: %w", err)
	}
	return cloneTemplate(created), nil
}

func (s *Service) Validate(ctx context.Context, templateID string, version int64, input ValidationInput) (ValidationResult, error) {
	template, err := s.GetTemplate(ctx, templateID, version)
	if err != nil {
		return ValidationResult{}, err
	}
	if input.Values == nil || len(input.Values) > MaximumFields*2 || len(input.MissingReferences) > MaximumFields {
		return ValidationResult{}, ErrInvalidInput
	}
	missing := make(map[string]bool, len(input.MissingReferences))
	for _, key := range input.MissingReferences {
		key = strings.TrimSpace(key)
		if !fieldKeyPattern.MatchString(key) {
			return ValidationResult{}, ErrInvalidInput
		}
		missing[key] = true
	}
	known := make(map[string]Field, len(template.Fields))
	for _, field := range template.Fields {
		known[field.Key] = field
	}
	for key := range missing {
		field, ok := known[key]
		if !ok || field.Type != FieldReference && field.Type != FieldAttachment {
			return ValidationResult{}, ErrInvalidInput
		}
	}
	result := ValidationResult{Status: ValidationValid, NormalizedValues: make(map[string]any), Errors: []FieldError{}, HoldingReferences: []HoldingReference{}}
	for _, field := range template.Fields {
		value, present := input.Values[field.Key]
		if !present || value == nil || strings.TrimSpace(fmt.Sprint(value)) == "" {
			if missing[field.Key] {
				result.Errors = append(result.Errors, fieldError(field, "reference", "Enter the unresolved record identifier."))
				continue
			}
			if field.Required {
				// A holding record always identifies a real stable target that can
				// later resolve. An omitted required relationship has no resolvable
				// identity and therefore remains invalid even when holding is allowed.
				result.Errors = append(result.Errors, fieldError(field, "required", "A value is required."))
			}
			continue
		}
		normalized, code, message := normalizeValue(field, value)
		if code != "" {
			result.Errors = append(result.Errors, fieldError(field, code, message))
			continue
		}
		if (field.Type == FieldReference || field.Type == FieldAttachment) && missing[field.Key] {
			if field.AllowHolding && input.AllowHoldingRecord {
				result.HoldingReferences = append(result.HoldingReferences, HoldingReference{Field: field.Key, ReferenceType: field.ReferenceType, Value: fmt.Sprint(normalized)})
			} else {
				result.Errors = append(result.Errors, fieldError(field, "reference_not_found", "The referenced record does not exist or is not visible."))
			}
		}
		result.NormalizedValues[field.Key] = normalized
	}
	for _, field := range template.Fields {
		if field.Type != FieldMoney {
			continue
		}
		if _, amountPresent := result.NormalizedValues[field.Key]; !amountPresent {
			continue
		}
		currency, ok := result.NormalizedValues[field.CurrencyField].(string)
		if !ok || !currencyPattern.MatchString(currency) {
			companion := known[field.CurrencyField]
			hasCompanionError := false
			for _, existing := range result.Errors {
				if existing.Field == companion.Key {
					hasCompanionError = true
					break
				}
			}
			if !hasCompanionError {
				result.Errors = append(result.Errors, fieldError(companion, "currency", "Enter a three-letter uppercase ISO currency code."))
			}
		}
	}
	for key := range input.Values {
		if _, ok := known[key]; !ok {
			result.Errors = append(result.Errors, FieldError{Field: key, Code: "unknown_field", Message: "This field is not defined by the selected template version."})
		}
	}
	sort.Slice(result.Errors, func(i, j int) bool { return result.Errors[i].Field < result.Errors[j].Field })
	sort.Slice(result.HoldingReferences, func(i, j int) bool { return result.HoldingReferences[i].Field < result.HoldingReferences[j].Field })
	if len(result.Errors) > 0 {
		result.Status = ValidationInvalid
	} else if len(result.HoldingReferences) > 0 {
		result.Status = ValidationHolding
	}
	return result, nil
}

func (s *Service) CSVTemplate(ctx context.Context, templateID string, version int64) ([]byte, error) {
	template, err := s.GetTemplate(ctx, templateID, version)
	if err != nil {
		return nil, err
	}
	var buffer bytes.Buffer
	writer := csv.NewWriter(&buffer)
	headers := make([]string, len(template.Fields))
	for index, field := range template.Fields {
		headers[index] = field.CSVHeader
	}
	if err := writer.Write(headers); err != nil {
		return nil, fmt.Errorf("write Patterns CSV header: %w", err)
	}
	writer.Flush()
	if err := writer.Error(); err != nil {
		return nil, fmt.Errorf("flush Patterns CSV: %w", err)
	}
	return buffer.Bytes(), nil
}

func normalizeFields(input []Field) ([]Field, error) {
	if len(input) == 0 || len(input) > MaximumFields {
		return nil, ErrInvalidInput
	}
	result := make([]Field, len(input))
	seen := make(map[string]bool, len(input))
	for index, field := range input {
		field.Key = strings.TrimSpace(field.Key)
		field.Label = strings.TrimSpace(field.Label)
		field.Help = strings.TrimSpace(field.Help)
		field.AccessibleLabel = strings.TrimSpace(field.AccessibleLabel)
		field.CSVHeader = strings.TrimSpace(field.CSVHeader)
		field.ReferenceType = strings.TrimSpace(field.ReferenceType)
		field.CurrencyField = strings.TrimSpace(field.CurrencyField)
		if field.AccessibleLabel == "" {
			field.AccessibleLabel = field.Label
		}
		if field.CSVHeader == "" {
			field.CSVHeader = field.Key
		}
		if seen[field.Key] || !fieldKeyPattern.MatchString(field.Key) || !validText(field.Label, 1, 120) ||
			!validText(field.Help, 0, 500) || !validText(field.AccessibleLabel, 1, 160) ||
			!validText(field.CSVHeader, 1, 160) || strings.ContainsRune("=+-@", rune(field.CSVHeader[0])) || !validFieldType(field.Type) ||
			field.MaximumLength < 0 || field.MaximumLength > 100_000 ||
			field.Minimum != nil && (math.IsInf(*field.Minimum, 0) || math.IsNaN(*field.Minimum)) ||
			field.Maximum != nil && (math.IsInf(*field.Maximum, 0) || math.IsNaN(*field.Maximum)) ||
			field.Minimum != nil && field.Maximum != nil && *field.Minimum > *field.Maximum {
			return nil, ErrInvalidInput
		}
		if field.Type == FieldEnum {
			if len(field.Options) == 0 || len(field.Options) > 100 {
				return nil, ErrInvalidInput
			}
			optionSeen := map[string]bool{}
			normalizedOptions := make([]string, len(field.Options))
			for optionIndex, option := range field.Options {
				option = strings.TrimSpace(option)
				if !validText(option, 1, 160) || optionSeen[option] {
					return nil, ErrInvalidInput
				}
				normalizedOptions[optionIndex], optionSeen[option] = option, true
			}
			field.Options = normalizedOptions
		} else if len(field.Options) > 0 {
			return nil, ErrInvalidInput
		}
		if field.Type == FieldReference || field.Type == FieldAttachment {
			if !recordTypePattern.MatchString(field.ReferenceType) {
				return nil, ErrInvalidInput
			}
		} else if field.ReferenceType != "" || field.AllowHolding {
			return nil, ErrInvalidInput
		}
		if field.Type == FieldMoney {
			if !fieldKeyPattern.MatchString(field.CurrencyField) {
				return nil, ErrInvalidInput
			}
		} else if field.CurrencyField != "" {
			return nil, ErrInvalidInput
		}
		if field.Type != FieldText && field.MaximumLength != 0 ||
			field.Type != FieldNumber && field.Type != FieldMoney && (field.Minimum != nil || field.Maximum != nil) {
			return nil, ErrInvalidInput
		}
		seen[field.Key] = true
		result[index] = field
	}
	for _, field := range result {
		if field.Type != FieldMoney {
			continue
		}
		_, ok := seen[field.CurrencyField]
		if !ok || field.CurrencyField == field.Key {
			return nil, ErrInvalidInput
		}
		var companion Field
		for _, candidate := range result {
			if candidate.Key == field.CurrencyField {
				companion = candidate
				break
			}
		}
		if companion.Type != FieldText && companion.Type != FieldEnum {
			return nil, ErrInvalidInput
		}
	}
	return result, nil
}

func normalizeValue(field Field, raw any) (any, string, string) {
	switch field.Type {
	case FieldText:
		value, ok := raw.(string)
		if !ok {
			return nil, "type", "Enter text."
		}
		value = strings.TrimSpace(value)
		maximum := field.MaximumLength
		if maximum == 0 {
			maximum = 10_000
		}
		if !validText(value, 0, maximum) {
			return nil, "length", fmt.Sprintf("Enter no more than %d characters.", maximum)
		}
		return value, "", ""
	case FieldEnum:
		value, ok := raw.(string)
		if !ok {
			return nil, "type", "Choose one of the available values."
		}
		value = strings.TrimSpace(value)
		for _, option := range field.Options {
			if value == option {
				return value, "", ""
			}
		}
		return nil, "enum", "Choose one of the available values."
	case FieldDate:
		value, ok := raw.(string)
		if !ok {
			return nil, "type", "Enter a date in YYYY-MM-DD format."
		}
		value = strings.TrimSpace(value)
		if _, err := time.Parse("2006-01-02", value); err != nil {
			return nil, "date", "Enter a valid date in YYYY-MM-DD format."
		}
		return value, "", ""
	case FieldReference, FieldAttachment:
		value, ok := raw.(string)
		if !ok || !stableIDPattern.MatchString(strings.TrimSpace(value)) {
			return nil, "reference", "Enter a valid record identifier."
		}
		return strings.TrimSpace(value), "", ""
	case FieldNumber, FieldMoney:
		if field.Type == FieldMoney {
			value, ok := exactMoney(raw)
			if !ok {
				return nil, "money", "Enter an exact integer amount in minor units."
			}
			if field.Minimum != nil && float64(value) < *field.Minimum || field.Maximum != nil && float64(value) > *field.Maximum {
				return nil, "range", "Enter a value within the allowed range."
			}
			return value, "", ""
		}
		value, ok := finiteNumber(raw)
		if !ok {
			return nil, "number", "Enter a valid number."
		}
		if field.Minimum != nil && value < *field.Minimum || field.Maximum != nil && value > *field.Maximum {
			return nil, "range", "Enter a value within the allowed range."
		}
		return value, "", ""
	default:
		return nil, "type", "This field type is not supported."
	}
}

const maximumExactJSONInteger int64 = 9_007_199_254_740_991

// exactMoney preserves the lexical integrity of JSON amounts. In particular,
// decimal and exponent syntax is never rounded through float64 before it is
// accepted as an integer amount in minor units.
func exactMoney(raw any) (int64, bool) {
	withinRange := func(value int64) (int64, bool) {
		return value, value >= -maximumExactJSONInteger && value <= maximumExactJSONInteger
	}
	switch candidate := raw.(type) {
	case json.Number:
		text := candidate.String()
		if text == "" || text == "-" || strings.ContainsAny(text, ".eE+") || text[0] == '0' && len(text) > 1 || strings.HasPrefix(text, "-0") && len(text) > 2 {
			return 0, false
		}
		value, err := strconv.ParseInt(text, 10, 64)
		if err != nil {
			return 0, false
		}
		return withinRange(value)
	case int:
		return withinRange(int64(candidate))
	case int8:
		return withinRange(int64(candidate))
	case int16:
		return withinRange(int64(candidate))
	case int32:
		return withinRange(int64(candidate))
	case int64:
		return withinRange(candidate)
	case uint:
		if uint64(candidate) > uint64(maximumExactJSONInteger) {
			return 0, false
		}
		return int64(candidate), true
	case uint8:
		return int64(candidate), true
	case uint16:
		return int64(candidate), true
	case uint32:
		return int64(candidate), true
	case uint64:
		if candidate > uint64(maximumExactJSONInteger) {
			return 0, false
		}
		return int64(candidate), true
	default:
		return 0, false
	}
}

func finiteNumber(raw any) (float64, bool) {
	var value float64
	switch candidate := raw.(type) {
	case float64:
		value = candidate
	case float32:
		value = float64(candidate)
	case int:
		value = float64(candidate)
	case int64:
		value = float64(candidate)
	case json.Number:
		parsed, err := candidate.Float64()
		if err != nil {
			return 0, false
		}
		value = parsed
	default:
		return 0, false
	}
	return value, !math.IsNaN(value) && !math.IsInf(value, 0)
}

func fieldError(field Field, code, message string) FieldError {
	return FieldError{Field: field.Key, Code: code, Message: field.AccessibleLabel + ": " + message}
}

func validFieldType(value FieldType) bool {
	return value == FieldText || value == FieldNumber || value == FieldDate || value == FieldMoney ||
		value == FieldEnum || value == FieldAttachment || value == FieldReference
}

func validText(value string, minimum, maximum int) bool {
	if !utf8.ValidString(value) || utf8.RuneCountInString(value) < minimum || utf8.RuneCountInString(value) > maximum {
		return false
	}
	for _, character := range value {
		if unicode.IsControl(character) && character != '\n' && character != '\t' {
			return false
		}
	}
	return true
}

func templateID(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value != "" {
		if !stableIDPattern.MatchString(value) {
			return "", ErrInvalidInput
		}
		return value, nil
	}
	buffer := make([]byte, 16)
	if _, err := rand.Read(buffer); err != nil {
		return "", fmt.Errorf("create Patterns template id: %w", err)
	}
	return hex.EncodeToString(buffer), nil
}

func normalizeExchangeTemplate(value ExchangeTemplate) (ExchangeTemplate, error) {
	value.ID = strings.TrimSpace(value.ID)
	value.RecordType = strings.ToLower(strings.TrimSpace(value.RecordType))
	value.Name = strings.TrimSpace(value.Name)
	if !stableIDPattern.MatchString(value.ID) || !recordTypePattern.MatchString(value.RecordType) ||
		!validText(value.Name, 1, 160) || len(value.Versions) == 0 || len(value.Versions) > MaximumExchangeVersions {
		return ExchangeTemplate{}, ErrInvalidInput
	}
	result := ExchangeTemplate{ID: value.ID, RecordType: value.RecordType, Name: value.Name, Versions: make([]ExchangeTemplateVersion, len(value.Versions))}
	approximateBytes := len(value.ID) + len(value.RecordType) + len(value.Name)
	for index, version := range value.Versions {
		version.Description = strings.TrimSpace(version.Description)
		fields, err := normalizeFields(version.Fields)
		if err != nil || version.Version != int64(index+1) || version.Status != StatusActive && version.Status != StatusRetired || !validText(version.Description, 0, 1000) {
			return ExchangeTemplate{}, ErrInvalidInput
		}
		version.Fields = fields
		approximateBytes += len(version.Description)
		for _, field := range fields {
			approximateBytes += len(field.Key) + len(field.Label) + len(field.Help) + len(field.ReferenceType) + len(field.AccessibleLabel) + len(field.CSVHeader) + len(field.CurrencyField)
			for _, option := range field.Options {
				approximateBytes += len(option)
			}
		}
		if approximateBytes > MaximumExchangeHistoryBytes {
			return ExchangeTemplate{}, ErrInvalidInput
		}
		result.Versions[index] = version
	}
	return result, nil
}

// ValidateExchangeTemplate applies the owning domain's complete portable
// history contract without granting any mutation capability.
func ValidateExchangeTemplate(value ExchangeTemplate) error {
	normalized, err := normalizeExchangeTemplate(value)
	if err != nil || !sameExchangeTemplate(normalized, value) {
		return ErrInvalidInput
	}
	return nil
}

func exchangeHistory(value ExchangeTemplate, organizationID string, occurredAt time.Time) []Template {
	result := make([]Template, len(value.Versions))
	for index, version := range value.Versions {
		result[index] = Template{ID: value.ID, OrganizationID: organizationID, RecordType: value.RecordType,
			Name: value.Name, Description: version.Description, Version: version.Version, BuiltIn: false,
			Status: version.Status, Fields: cloneFields(version.Fields), CreatedBy: "system:exchange", CreatedAt: occurredAt}
	}
	return result
}

func exchangeAuditTemplate(value ExchangeTemplate, organizationID string, occurredAt time.Time) Template {
	history := exchangeHistory(value, organizationID, occurredAt)
	return history[len(history)-1]
}

func sameExchangeTemplate(left, right ExchangeTemplate) bool {
	leftEncoded, leftErr := json.Marshal(left)
	rightEncoded, rightErr := json.Marshal(right)
	return leftErr == nil && rightErr == nil && bytes.Equal(leftEncoded, rightEncoded)
}

func (s *Service) checkWrite(ctx context.Context, id string) error {
	if _, importing := ctx.Value(exchangeImportContextKey{}).(exchangeImportContext); importing || s.writes == nil {
		return nil
	}
	return s.writes.CheckResourceWrite(ctx, "patterns.template", id)
}

func actorFromContext(ctx context.Context) string {
	if _, importing := ctx.Value(exchangeImportContextKey{}).(exchangeImportContext); importing {
		return "system:exchange"
	}
	if scope, ok := foundation.ScopeFromContext(ctx); ok && strings.TrimSpace(scope.ActorID) != "" {
		return scope.ActorID
	}
	return "system:patterns"
}

func (s *Service) audit(ctx context.Context, action string, template Template, metadata map[string]string) error {
	scope, ok := foundation.ScopeFromContext(ctx)
	state, importing := ctx.Value(exchangeImportContextKey{}).(exchangeImportContext)
	if importing {
		scope = foundation.Scope{OrganizationID: s.organizationID, ActorID: "system:exchange", CorrelationID: state.operation.Token}
		ok = true
	}
	if !ok || scope.CorrelationID == "" {
		correlationID, err := foundation.NewCorrelationID()
		if err != nil {
			return err
		}
		scope = foundation.Scope{OrganizationID: s.organizationID, ActorID: actorFromContext(ctx), CorrelationID: correlationID}
		ctx = foundation.WithScope(ctx, scope)
	}
	if metadata == nil {
		metadata = map[string]string{}
	}
	metadata["requirementId"] = RequirementID
	metadata["recordType"] = template.RecordType
	metadata["version"] = strconv.FormatInt(template.Version, 10)
	eventID := ""
	occurredAt := s.now().UTC()
	if importing {
		digest := sha256.Sum256([]byte(strings.Join([]string{s.organizationID, state.operation.Token, action, "template", template.ID}, "\x00")))
		eventID = hex.EncodeToString(digest[:])
		occurredAt = state.operation.OccurredAt
	} else {
		var err error
		eventID, err = foundation.NewCorrelationID()
		if err != nil {
			return err
		}
	}
	return s.auditor.Record(ctx, foundation.AuditEvent{ID: eventID, OrganizationID: s.organizationID,
		ActorID: actorFromContext(ctx), CorrelationID: scope.CorrelationID, Action: action,
		ResourceType: "template", ResourceID: template.ID, OccurredAt: occurredAt, Metadata: metadata})
}

func cloneTemplate(template Template) Template {
	template.Fields = cloneFields(template.Fields)
	return template
}

func cloneFields(fields []Field) []Field {
	result := make([]Field, len(fields))
	for index, field := range fields {
		field.Options = append([]string(nil), field.Options...)
		if field.Minimum != nil {
			value := *field.Minimum
			field.Minimum = &value
		}
		if field.Maximum != nil {
			value := *field.Maximum
			field.Maximum = &value
		}
		result[index] = field
	}
	return result
}
