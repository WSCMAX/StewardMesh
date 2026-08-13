package exchange

// Requirements: REQ-EXCHANGE-001, REQ-PATTERNS-001. Features: migration.packages, templates.schemas. GitHub: #9, #8.

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/maxlemke/stewardmesh/internal/foundation"
	"github.com/maxlemke/stewardmesh/internal/guard"
	"github.com/maxlemke/stewardmesh/internal/patterns"
)

var errProcessingLeaseLost = errors.New("Exchange processing lease was lost")

type ServiceConfig struct {
	OrganizationID  string
	SourceSystemID  string
	Schemas         SchemaRegistry
	Now             func() time.Time
	ProcessingLease time.Duration
}

type ownershipManager interface {
	ImportedResourceOwnership(ctx context.Context, organizationID, resourceType, resourceID string) (guard.ResourceOwnership, error)
	RegisterImportedResourceOwnership(ctx context.Context, actorID string, input guard.ResourceOwnershipInput) (guard.ResourceOwnership, bool, error)
	DeleteImportedResourceOwnership(ctx context.Context, ownership guard.ResourceOwnership) error
}

type Service struct {
	store           Store
	auditor         foundation.Auditor
	guard           ownershipManager
	organizationID  string
	sourceSystemID  string
	now             func() time.Time
	processingLease time.Duration
	schemas         SchemaRegistry
	providers       map[string]Provider
	providerList    []Provider
}

func NewService(store Store, auditor foundation.Auditor, ownership ownershipManager, configuration ServiceConfig, providers ...Provider) (*Service, error) {
	configuration.OrganizationID = strings.TrimSpace(configuration.OrganizationID)
	configuration.SourceSystemID = strings.TrimSpace(configuration.SourceSystemID)
	if store == nil || auditor == nil || ownership == nil || configuration.Schemas == nil || !stableIDPattern.MatchString(configuration.OrganizationID) || !stableIDPattern.MatchString(configuration.SourceSystemID) {
		return nil, errors.New("Exchange store, auditor, ownership service, Patterns schemas, organization, and source system are required")
	}
	if configuration.Now == nil {
		configuration.Now = func() time.Time { return time.Now().UTC() }
	}
	if configuration.ProcessingLease <= 0 {
		configuration.ProcessingLease = ProcessingLease
	}
	service := &Service{
		store: store, auditor: auditor, guard: ownership, organizationID: configuration.OrganizationID,
		sourceSystemID: configuration.SourceSystemID, now: configuration.Now, processingLease: configuration.ProcessingLease,
		schemas: configuration.Schemas, providers: make(map[string]Provider),
	}
	for _, provider := range providers {
		if provider == nil || len(provider.Types()) == 0 {
			return nil, errors.New("Exchange providers must expose at least one record type")
		}
		seen := make(map[string]struct{})
		service.providerList = append(service.providerList, provider)
		for _, recordType := range provider.Types() {
			if !resourceTypePattern.MatchString(recordType) {
				return nil, fmt.Errorf("%w: provider record type is invalid", ErrInvalidInput)
			}
			if _, duplicate := seen[recordType]; duplicate {
				return nil, fmt.Errorf("%w: provider repeats record type", ErrInvalidInput)
			}
			seen[recordType] = struct{}{}
			if _, duplicate := service.providers[recordType]; duplicate {
				return nil, fmt.Errorf("%w: record type has multiple providers", ErrConflict)
			}
			service.providers[recordType] = provider
		}
	}
	if len(service.providers) == 0 {
		return nil, errors.New("at least one Exchange provider is required")
	}
	return service, nil
}

func (s *Service) SourceSystemID() string { return s.sourceSystemID }

func (s *Service) ListRecords(ctx context.Context) ([]RecordDescriptor, error) {
	records, _, err := s.catalog(ctx)
	if err != nil {
		return nil, err
	}
	result := make([]RecordDescriptor, 0, len(records))
	keys := sortedRecordKeys(records)
	for _, key := range keys {
		record := records[key]
		result = append(result, RecordDescriptor{
			Type: record.Type, ID: record.ID, Revision: record.Revision, TemplateID: record.TemplateID, TemplateVersion: record.TemplateVersion,
			Dependencies: append([]Reference{}, record.Dependencies...), HasFile: record.File != nil,
		})
	}
	return result, nil
}

func (s *Service) ListPackages(ctx context.Context, limit int) ([]Package, error) {
	if limit == 0 {
		limit = 25
	}
	if limit < 1 || limit > MaximumHistory {
		return nil, ErrInvalidInput
	}
	items, err := s.store.ListPackages(ctx, s.organizationID, limit)
	if err != nil {
		return nil, fmt.Errorf("list Exchange packages: %w", err)
	}
	if items == nil {
		items = []Package{}
	}
	return items, nil
}

func (s *Service) Export(ctx context.Context, actorID string, request ExportRequest) (ExportArtifact, error) {
	actorID = strings.TrimSpace(actorID)
	if actorID == "" || len(actorID) > 128 || len(request.Selection) == 0 || len(request.Selection) > MaximumSelections ||
		(request.FileMode != FileModeMetadata && request.FileMode != FileModeInclude) {
		return ExportArtifact{}, ErrInvalidInput
	}
	records, owners, err := s.catalog(ctx)
	if err != nil {
		return ExportArtifact{}, err
	}
	selected := make(map[string]Record)
	explicit := make(map[string]struct{}, len(request.Selection))
	for _, reference := range request.Selection {
		if _, duplicate := explicit[reference.Key()]; duplicate {
			return ExportArtifact{}, ErrInvalidInput
		}
		explicit[reference.Key()] = struct{}{}
	}
	queue := append([]Reference(nil), request.Selection...)
	for len(queue) > 0 {
		reference := queue[0]
		queue = queue[1:]
		if !resourceTypePattern.MatchString(reference.Type) || !stableIDPattern.MatchString(reference.ID) {
			return ExportArtifact{}, ErrInvalidInput
		}
		key := reference.Key()
		if _, duplicate := selected[key]; duplicate {
			continue
		}
		record, ok := records[key]
		if !ok {
			return ExportArtifact{}, ErrNotFound
		}
		selected[key] = cloneRecord(record)
		if request.IncludeDependencies {
			for _, dependency := range record.Dependencies {
				if _, available := records[dependency.Key()]; available {
					queue = append(queue, dependency)
				}
			}
		}
		if len(selected) > MaximumRecords {
			return ExportArtifact{}, ErrTooLarge
		}
	}
	ordered, err := topologicalRecords(selected)
	if err != nil {
		return ExportArtifact{}, err
	}
	files := make(map[string][]byte)
	for index := range ordered {
		record := &ordered[index]
		if ownership, ok := owners[Reference{Type: record.Type, ID: record.ID}.Key()]; ok {
			record.Ownership = ownership
		}
		if record.File == nil || request.FileMode != FileModeInclude {
			continue
		}
		provider := s.providers[record.Type]
		reader, ok := provider.(FileReader)
		if !ok {
			return ExportArtifact{}, ErrInvalidInput
		}
		content, err := reader.ReadRecordFile(ctx, *record)
		if err != nil {
			return ExportArtifact{}, fmt.Errorf("read Exchange file: %w", err)
		}
		if int64(len(content)) > MaximumFileBytes {
			return ExportArtifact{}, ErrTooLarge
		}
		files[Reference{Type: record.Type, ID: record.ID}.Key()] = content
	}
	packageID, err := foundation.NewCorrelationID()
	if err != nil {
		return ExportArtifact{}, fmt.Errorf("create Exchange package id: %w", err)
	}
	now := normalizedNow(s.now())
	artifact, sealed, err := encodeArchive(Manifest{
		SchemaVersion: SchemaVersion, PackageID: packageID, SourceSystemID: s.sourceSystemID,
		ExportedAt: now, FileMode: request.FileMode, Schemas: schemaReferences(ordered), Records: ordered,
	}, files)
	if err != nil {
		return ExportArtifact{}, err
	}
	outcomes := make([]RecordOutcome, 0, len(sealed.Records))
	for _, record := range sealed.Records {
		outcomes = append(outcomes, RecordOutcome{
			Type: record.Type, ID: record.ID, Revision: record.Revision, Checksum: record.Checksum,
			Status: OutcomeUnchanged, MissingDependencies: []Reference{},
		})
	}
	receipt := Package{
		OrganizationID: s.organizationID, PackageID: packageID, Direction: DirectionExport,
		SchemaVersion: SchemaVersion, SourceSystemID: s.sourceSystemID, ArchiveSHA256: artifact.SHA256,
		SizeBytes: int64(len(artifact.Bytes)), FileMode: request.FileMode, Status: StatusCompleted,
		RecordCount: len(sealed.Records), FileCount: countManifestFiles(sealed), UnchangedCount: len(sealed.Records),
		Records: outcomes, CreatedBy: actorID, CreatedAt: now, UpdatedAt: now,
	}
	if _, created, err := s.store.CreatePackage(ctx, receipt); err != nil || !created {
		if err == nil {
			err = ErrConflict
		}
		return ExportArtifact{}, fmt.Errorf("record Exchange export: %w", err)
	}
	if err := s.audit(ctx, actorID, "exchange.package.exported", receipt); err != nil {
		return ExportArtifact{}, fmt.Errorf("audit Exchange export: %w", err)
	}
	return artifact, nil
}

func (s *Service) Import(ctx context.Context, actorID string, contents []byte) (ImportResult, error) {
	actorID = strings.TrimSpace(actorID)
	if actorID == "" || len(actorID) > 128 {
		return ImportResult{}, ErrInvalidInput
	}
	decoded, archiveChecksum, err := decodeArchive(contents)
	if err != nil {
		return ImportResult{}, err
	}
	now := normalizedNow(s.now())
	pending := Package{
		OrganizationID: s.organizationID, PackageID: decoded.Manifest.PackageID, Direction: DirectionImport,
		SchemaVersion: decoded.Manifest.SchemaVersion, SourceSystemID: decoded.Manifest.SourceSystemID,
		ArchiveSHA256: archiveChecksum, SizeBytes: int64(len(contents)), FileMode: decoded.Manifest.FileMode,
		Status: StatusProcessing, RecordCount: len(decoded.Manifest.Records), FileCount: countManifestFiles(decoded.Manifest),
		Records: []RecordOutcome{}, CreatedBy: actorID, CreatedAt: now, UpdatedAt: now,
	}
	stored, created, err := s.store.CreatePackage(ctx, pending)
	if err != nil {
		return ImportResult{}, fmt.Errorf("reserve Exchange import: %w", err)
	}
	pending = stored
	if !created {
		if stored.ArchiveSHA256 != archiveChecksum || stored.SourceSystemID != decoded.Manifest.SourceSystemID ||
			stored.SchemaVersion != decoded.Manifest.SchemaVersion || stored.SizeBytes != int64(len(contents)) ||
			stored.FileMode != decoded.Manifest.FileMode || stored.RecordCount != len(decoded.Manifest.Records) ||
			stored.FileCount != countManifestFiles(decoded.Manifest) {
			return ImportResult{}, ErrConflict
		}
		switch stored.Status {
		case StatusCompleted:
			return ImportResult{Package: stored, Replay: true}, nil
		case StatusProcessing:
			if now.Before(stored.UpdatedAt.Add(s.processingLease)) {
				return ImportResult{}, ErrConflict
			}
			pending = retryPackage(stored, now)
			stored, err = s.store.UpdatePackage(ctx, pending, stored.UpdatedAt)
			if err != nil {
				return ImportResult{}, fmt.Errorf("take over stale Exchange import: %w", err)
			}
			pending = stored
		case StatusHolding, StatusFailed:
			pending = retryPackage(stored, now)
			stored, err = s.store.UpdatePackage(ctx, pending, stored.UpdatedAt)
			if err != nil {
				return ImportResult{}, fmt.Errorf("retry Exchange import: %w", err)
			}
			pending = stored
		default:
			return ImportResult{}, ErrConflict
		}
	}
	recordMap := make(map[string]Record, len(decoded.Manifest.Records))
	for _, record := range decoded.Manifest.Records {
		recordMap[Reference{Type: record.Type, ID: record.ID}.Key()] = cloneRecord(record)
	}
	ordered, err := topologicalRecords(recordMap)
	if err != nil {
		return ImportResult{}, s.failImport(ctx, actorID, pending, err)
	}
	outcomesByKey := make(map[string]RecordOutcome, len(ordered))
	for _, outcome := range pending.Records {
		outcomesByKey[Reference{Type: outcome.Type, ID: outcome.ID}.Key()] = cloneOutcome(outcome)
	}
	progressByKey := make(map[string]ImportProgress, len(pending.Progress))
	for _, progress := range pending.Progress {
		progressByKey[progress.key()] = progress
	}
	preflight, err := s.preflightImport(ctx, ordered, recordMap, outcomesByKey, progressByKey)
	if err != nil {
		return ImportResult{}, s.failImport(ctx, actorID, pending, err)
	}
	for _, original := range ordered {
		key := Reference{Type: original.Type, ID: original.ID}.Key()
		prepared := preflight[key]
		record := prepared.Record
		progress, hasProgress := progressByKey[key]
		if _, alreadyDurable := outcomesByKey[key]; alreadyDurable && !hasProgress {
			continue
		}
		provider := s.providers[record.Type]
		if !hasProgress {
			if prepared.Validation == patterns.ValidationHolding || len(prepared.MissingDependencies) > 0 {
				outcome := RecordOutcome{Type: record.Type, ID: record.ID, Revision: record.Revision, Checksum: record.Checksum, Status: OutcomeHolding, MissingDependencies: append([]Reference(nil), prepared.MissingDependencies...)}
				outcomesByKey[key] = outcome
				continue
			}
		}
		if provider == nil {
			return ImportResult{}, s.failImport(ctx, actorID, pending, ErrConflict)
		}
		var file []byte
		if record.File != nil {
			file = decoded.Files[record.File.Entry]
		}
		if !hasProgress {
			exact, inspectErr := provider.ImportRecordExists(ctx, record, file)
			if inspectErr != nil {
				return ImportResult{}, s.failImport(ctx, actorID, pending, inspectErr)
			}
			progress = ImportProgress{
				Type: record.Type, ID: record.ID, Checksum: record.Checksum,
				OperationToken: importOperationToken(pending, record), Phase: progressIntent,
				ExpectedCreated: !exact,
			}
			candidate := pending
			upsertImportProgress(&candidate, progress)
			checkpointed, checkpointErr := s.checkpointImport(ctx, candidate, ordered, outcomesByKey)
			if checkpointErr != nil {
				return ImportResult{}, s.failImport(ctx, actorID, pending, fmt.Errorf("reserve Exchange record intent: %w", checkpointErr))
			}
			pending = checkpointed
			progressByKey[key] = progress
		}
		provenance := record.Provenance
		if provenance.SourceSystemID == "" {
			provenance.SourceSystemID = decoded.Manifest.SourceSystemID
		}
		if provenance.SourceRecordID == "" {
			provenance.SourceRecordID = key
		}
		ownership, ownershipCreated, lockErr := s.guard.RegisterImportedResourceOwnership(ctx, actorID, guard.ResourceOwnershipInput{
			ResourceType: record.Type, ResourceID: record.ID,
			SourceSystemID: provenance.SourceSystemID, SourceRecordID: provenance.SourceRecordID,
		})
		if lockErr != nil {
			candidate := pending
			if progress.Phase == progressIntent && !progress.OwnershipReady {
				removeImportProgress(&candidate, key)
			}
			return ImportResult{}, s.failImport(ctx, actorID, candidate, lockErr)
		}
		if !progress.OwnershipReady || ownershipCreated || progress.WriteLocked != ownership.WriteLocked {
			progress.OwnershipReady = true
			progress.OwnershipCreated = progress.OwnershipCreated || ownershipCreated
			progress.WriteLocked = ownership.WriteLocked
			candidate := pending
			upsertImportProgress(&candidate, progress)
			checkpointed, checkpointErr := s.checkpointImport(ctx, candidate, ordered, outcomesByKey)
			if checkpointErr != nil {
				var cleanupErr error
				if ownershipCreated {
					cleanupErr = s.guard.DeleteImportedResourceOwnership(ctx, ownership)
				}
				failureCandidate := pending
				if cleanupErr == nil {
					removeImportProgress(&failureCandidate, key)
				}
				return ImportResult{}, s.failImport(ctx, actorID, failureCandidate, errors.Join(fmt.Errorf("checkpoint Exchange ownership intent: %w", checkpointErr), cleanupErr))
			}
			pending = checkpointed
			progressByKey[key] = progress
		}
		providerResult, refreshed, importErr := s.importRecordWithHeartbeat(ctx, pending, provider, ProviderImportOperation{
			Token: progress.OperationToken, Repair: progress.Phase == progressCommitted,
			ExpectedCreated: progress.ExpectedCreated, OccurredAt: pending.CreatedAt,
		}, decoded.Manifest.SourceSystemID, record, file)
		pending = refreshed
		if providerResult.Committed {
			status := OutcomeUnchanged
			if progress.ExpectedCreated {
				status = OutcomeCreated
			}
			outcome := RecordOutcome{
				Type: record.Type, ID: record.ID, Revision: record.Revision, Checksum: record.Checksum,
				Status: status, MissingDependencies: []Reference{}, WriteLocked: progress.WriteLocked,
			}
			outcomesByKey[key] = outcome
			progress.Phase = progressCommitted
			candidate := pending
			upsertImportProgress(&candidate, progress)
			checkpointed, checkpointErr := s.checkpointImport(ctx, candidate, ordered, outcomesByKey)
			if checkpointErr != nil {
				setPackageOutcomes(&candidate, orderedOutcomes(ordered, outcomesByKey, false))
				return ImportResult{}, s.failImport(ctx, actorID, candidate, errors.Join(importErr, fmt.Errorf("checkpoint committed Exchange record: %w", checkpointErr)))
			}
			pending = checkpointed
			progressByKey[key] = progress
			if importErr != nil {
				return ImportResult{}, s.failImport(ctx, actorID, pending, importErr)
			}
			candidate = pending
			removeImportProgress(&candidate, key)
			checkpointed, checkpointErr = s.checkpointImport(ctx, candidate, ordered, outcomesByKey)
			if checkpointErr != nil {
				return ImportResult{}, s.failImport(ctx, actorID, pending, fmt.Errorf("complete Exchange record intent: %w", checkpointErr))
			}
			pending = checkpointed
			delete(progressByKey, key)
			continue
		}
		if importErr != nil {
			if errors.Is(importErr, errProcessingLeaseLost) {
				return ImportResult{}, errors.Join(ErrConflict, importErr)
			}
			var cleanupErr error
			if progress.OwnershipCreated {
				cleanupErr = s.guard.DeleteImportedResourceOwnership(ctx, ownership)
			}
			if cleanupErr != nil {
				return ImportResult{}, s.failImport(ctx, actorID, pending, errors.Join(importErr, cleanupErr))
			}
			candidate := pending
			removeImportProgress(&candidate, key)
			if errors.Is(importErr, ErrDependencyMissing) {
				checkpointed, checkpointErr := s.checkpointImport(ctx, candidate, ordered, outcomesByKey)
				if checkpointErr != nil {
					return ImportResult{}, s.failImport(ctx, actorID, candidate, errors.Join(importErr, fmt.Errorf("compensate Exchange record intent: %w", checkpointErr)))
				}
				pending = checkpointed
				delete(progressByKey, key)
				outcome := RecordOutcome{Type: record.Type, ID: record.ID, Revision: record.Revision, Checksum: record.Checksum, Status: OutcomeHolding, MissingDependencies: append([]Reference(nil), record.Dependencies...)}
				outcomesByKey[key] = outcome
				continue
			}
			return ImportResult{}, s.failImport(ctx, actorID, candidate, importErr)
		}
		return ImportResult{}, s.failImport(ctx, actorID, pending, ErrConflict)
	}
	expectedUpdatedAt := pending.UpdatedAt
	setPackageOutcomes(&pending, orderedOutcomes(ordered, outcomesByKey, true))
	pending.Status = StatusCompleted
	if pending.HoldingCount > 0 {
		pending.Status = StatusHolding
	}
	pending.UpdatedAt = nextUpdatedAt(s.now(), expectedUpdatedAt)
	completed, err := s.store.UpdatePackage(ctx, pending, expectedUpdatedAt)
	if err != nil {
		return ImportResult{}, fmt.Errorf("complete Exchange import: %w", err)
	}
	if err := s.audit(ctx, actorID, "exchange.package.imported", completed); err != nil {
		return ImportResult{}, fmt.Errorf("audit Exchange import: %w", err)
	}
	return ImportResult{Package: completed}, nil
}

func retryPackage(stored Package, now time.Time) Package {
	pending := stored
	pending.Status = StatusProcessing
	pending.ErrorCode = ""
	setPackageOutcomes(&pending, successfulOutcomes(stored.Records))
	pending.UpdatedAt = nextUpdatedAt(now, stored.UpdatedAt)
	return pending
}

func (s *Service) checkpointImport(ctx context.Context, pending Package, ordered []Record, outcomes map[string]RecordOutcome) (Package, error) {
	expected := pending.UpdatedAt
	setPackageOutcomes(&pending, orderedOutcomes(ordered, outcomes, false))
	pending.Status = StatusProcessing
	pending.ErrorCode = ""
	pending.UpdatedAt = nextUpdatedAt(s.now(), expected)
	checkpointed, err := s.store.UpdatePackage(ctx, pending, expected)
	if err != nil {
		return Package{}, err
	}
	return checkpointed, nil
}

type providerCall struct {
	result ProviderImportResult
	err    error
}

func (s *Service) importRecordWithHeartbeat(
	ctx context.Context,
	pending Package,
	provider Provider,
	operation ProviderImportOperation,
	sourceSystemID string,
	record Record,
	file []byte,
) (ProviderImportResult, Package, error) {
	callContext, cancel := context.WithCancel(ctx)
	defer cancel()
	done := make(chan providerCall, 1)
	go func() {
		result, err := provider.ImportRecord(callContext, operation, sourceSystemID, record, file)
		done <- providerCall{result: result, err: err}
	}()
	interval := s.processingLease / 3
	if interval <= 0 {
		interval = time.Millisecond
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	current := pending
	for {
		select {
		case call := <-done:
			return call.result, current, call.err
		case <-ctx.Done():
			cancel()
			call := <-done
			return call.result, current, errors.Join(call.err, ctx.Err())
		case <-ticker.C:
			expected := current.UpdatedAt
			candidate := current
			candidate.Status = StatusProcessing
			candidate.ErrorCode = ""
			candidate.UpdatedAt = nextUpdatedAt(s.now(), expected)
			refreshed, err := s.store.UpdatePackage(ctx, candidate, expected)
			if err == nil {
				current = refreshed
				continue
			}
			cancel()
			call := <-done
			return call.result, current, errors.Join(call.err, errProcessingLeaseLost, fmt.Errorf("renew Exchange processing lease: %w", err))
		}
	}
}

func importOperationToken(value Package, record Record) string {
	digest := sha256.Sum256([]byte(strings.Join([]string{
		value.OrganizationID, value.PackageID, value.ArchiveSHA256, record.Type, record.ID, record.Checksum,
	}, "\x00")))
	return hex.EncodeToString(digest[:])
}

func upsertImportProgress(value *Package, progress ImportProgress) {
	for index := range value.Progress {
		if value.Progress[index].key() == progress.key() {
			value.Progress[index] = progress
			return
		}
	}
	value.Progress = append(value.Progress, progress)
	sort.Slice(value.Progress, func(i, j int) bool { return value.Progress[i].key() < value.Progress[j].key() })
}

func removeImportProgress(value *Package, key string) {
	for index := range value.Progress {
		if value.Progress[index].key() == key {
			value.Progress = append(value.Progress[:index], value.Progress[index+1:]...)
			return
		}
	}
}

func orderedOutcomes(records []Record, byKey map[string]RecordOutcome, includeHolding bool) []RecordOutcome {
	result := make([]RecordOutcome, 0, len(byKey))
	for _, record := range records {
		outcome, ok := byKey[Reference{Type: record.Type, ID: record.ID}.Key()]
		if !ok || (!includeHolding && outcome.Status == OutcomeHolding) {
			continue
		}
		result = append(result, cloneOutcome(outcome))
	}
	return result
}

func setPackageOutcomes(value *Package, outcomes []RecordOutcome) {
	value.Records = outcomes
	value.CreatedCount = 0
	value.UnchangedCount = 0
	value.HoldingCount = 0
	for _, outcome := range outcomes {
		switch outcome.Status {
		case OutcomeCreated:
			value.CreatedCount++
		case OutcomeUnchanged:
			value.UnchangedCount++
		case OutcomeHolding:
			value.HoldingCount++
		}
	}
}

func (s *Service) failImport(ctx context.Context, actorID string, pending Package, cause error) error {
	expected := pending.UpdatedAt
	pending.Status = StatusFailed
	pending.ErrorCode = archiveErrorCode(cause)
	pending.UpdatedAt = nextUpdatedAt(s.now(), expected)
	failed, updateErr := s.store.UpdatePackage(ctx, pending, expected)
	if updateErr == nil {
		updateErr = s.audit(ctx, actorID, "exchange.package.import_failed", failed)
	}
	return errors.Join(cause, updateErr)
}

type importSchemaPreflight struct {
	Record              Record
	Validation          patterns.ValidationStatus
	MissingDependencies []Reference
}

// preflightImport resolves and validates every immutable Patterns schema before
// the durable loop reserves its first record intent or touches Guard/provider
// state. The second pass is still read-only: it classifies dependency-driven
// holdings in topological order and caches normalized payloads for the write
// loop. This prevents a later bad schema from creating a partial import.
func (s *Service) preflightImport(
	ctx context.Context,
	ordered []Record,
	records map[string]Record,
	durable map[string]RecordOutcome,
	progress map[string]ImportProgress,
) (map[string]importSchemaPreflight, error) {
	prepared := make(map[string]importSchemaPreflight, len(ordered))
	for _, original := range ordered {
		record := cloneRecord(original)
		validation, normalizedPayload, err := s.validateSchema(ctx, record, nil, true)
		if err != nil || validation.Status == patterns.ValidationInvalid {
			return nil, errors.Join(ErrInvalidInput, err)
		}
		record.Payload = normalizedPayload
		prepared[Reference{Type: record.Type, ID: record.ID}.Key()] = importSchemaPreflight{
			Record: record, Validation: validation.Status,
			MissingDependencies: holdingReferences(validation.HoldingReferences, nil),
		}
	}

	classified := make(map[string]RecordOutcome, len(ordered))
	for _, original := range ordered {
		key := Reference{Type: original.Type, ID: original.ID}.Key()
		item := prepared[key]
		_, hasProgress := progress[key]
		prior, alreadyDurable := durable[key]
		if alreadyDurable || hasProgress {
			if item.Validation == patterns.ValidationHolding || len(item.MissingDependencies) != 0 {
				return nil, ErrConflict
			}
			if hasProgress && s.providers[item.Record.Type] == nil {
				return nil, ErrConflict
			}
			if alreadyDurable {
				classified[key] = cloneOutcome(prior)
			} else {
				classified[key] = RecordOutcome{Type: item.Record.Type, ID: item.Record.ID, Status: OutcomeUnchanged}
			}
			continue
		}

		missing, err := s.missingDependencies(ctx, item.Record, records, classified)
		if err != nil {
			return nil, err
		}
		provider := s.providers[item.Record.Type]
		if provider == nil {
			missing = append(missing, Reference{Type: "provider", ID: item.Record.Type})
		}
		if provider != nil && item.Record.File != nil && item.Record.File.Entry == "" {
			exact := false
			if resolver, ok := provider.(MetadataOnlyResolver); ok {
				exact, err = resolver.MetadataOnlyRecordExists(ctx, item.Record)
				if err != nil {
					return nil, err
				}
			}
			if !exact {
				missing = append(missing, Reference{Type: "exchange.file", ID: item.Record.File.SHA256})
			}
		}
		missing = normalizeReferences(missing)
		validation, normalizedPayload, err := s.validateSchema(ctx, item.Record, missing, true)
		if err != nil || validation.Status == patterns.ValidationInvalid {
			return nil, errors.Join(ErrInvalidInput, err)
		}
		item.Record.Payload = normalizedPayload
		item.Validation = validation.Status
		missing = append(missing, holdingReferences(validation.HoldingReferences, missing)...)
		missing = normalizeReferences(missing)
		if len(missing) > MaximumOutcomeDependencies {
			return nil, ErrTooLarge
		}
		if validation.Status == patterns.ValidationHolding && len(missing) == 0 {
			return nil, ErrInvalidInput
		}
		item.MissingDependencies = missing
		prepared[key] = item
		status := OutcomeUnchanged
		if validation.Status == patterns.ValidationHolding || len(missing) > 0 {
			status = OutcomeHolding
		}
		classified[key] = RecordOutcome{Type: item.Record.Type, ID: item.Record.ID, Status: status}
	}
	return prepared, nil
}

func (s *Service) missingDependencies(ctx context.Context, record Record, records map[string]Record, outcomes map[string]RecordOutcome) ([]Reference, error) {
	missing := make([]Reference, 0)
	for _, dependency := range record.Dependencies {
		if _, packaged := records[dependency.Key()]; packaged {
			outcome, completed := outcomes[dependency.Key()]
			if !completed || outcome.Status == OutcomeHolding {
				missing = append(missing, dependency)
			}
			continue
		}
		exists, handled, err := s.dependencyExists(ctx, dependency)
		if err != nil {
			return nil, err
		}
		if !handled || !exists {
			missing = append(missing, dependency)
		}
	}
	return missing, nil
}

func (s *Service) dependencyExists(ctx context.Context, dependency Reference) (exists bool, handled bool, err error) {
	if provider := s.providers[dependency.Type]; provider != nil {
		exists, err := provider.Exists(ctx, dependency)
		return exists, true, err
	}
	for _, provider := range s.providerList {
		resolver, ok := provider.(DependencyResolver)
		if !ok {
			continue
		}
		resolved, found, resolveErr := resolver.DependencyExists(ctx, dependency)
		if resolveErr != nil {
			return false, true, resolveErr
		}
		if !resolved {
			continue
		}
		if handled {
			return false, true, ErrConflict
		}
		handled, exists = true, found
	}
	return exists, handled, nil
}

func (s *Service) catalog(ctx context.Context) (map[string]Record, map[string]OwnershipMetadata, error) {
	records := make(map[string]Record)
	owners := make(map[string]OwnershipMetadata)
	for _, provider := range s.providerList {
		items, err := provider.ListRecords(ctx)
		if err != nil {
			return nil, nil, fmt.Errorf("list Exchange provider records: %w", err)
		}
		for _, item := range items {
			item = cloneRecord(item)
			item.Checksum = ""
			if item.Dependencies == nil {
				item.Dependencies = []Reference{}
			}
			sort.Slice(item.Dependencies, func(i, j int) bool { return item.Dependencies[i].Key() < item.Dependencies[j].Key() })
			if item.Provenance.SourceSystemID == "" {
				item.Provenance = Provenance{SourceSystemID: s.sourceSystemID, SourceRecordID: Reference{Type: item.Type, ID: item.ID}.Key()}
			}
			if item.Ownership.State == "" {
				item.Ownership.State = "local"
			}
			template, err := s.schemas.ActiveTemplateForRecordType(ctx, item.Type)
			if err != nil || template.RecordType != item.Type || template.Status != patterns.StatusActive {
				return nil, nil, fmt.Errorf("resolve Exchange Patterns schema: %w", errors.Join(ErrInvalidInput, err))
			}
			item.TemplateID, item.TemplateVersion = template.ID, template.Version
			if err := validateRecord(item); err != nil {
				return nil, nil, fmt.Errorf("validate Exchange provider record: %w", err)
			}
			validation, normalizedPayload, err := s.validateSchema(ctx, item, nil, false)
			if err != nil || validation.Status != patterns.ValidationValid {
				return nil, nil, fmt.Errorf("validate Exchange provider schema: %w", errors.Join(ErrInvalidInput, err))
			}
			item.Payload = normalizedPayload
			key := Reference{Type: item.Type, ID: item.ID}.Key()
			if _, duplicate := records[key]; duplicate {
				return nil, nil, ErrConflict
			}
			ownership, err := s.guard.ImportedResourceOwnership(ctx, s.organizationID, item.Type, item.ID)
			switch {
			case err == nil:
				state := "claimed"
				if ownership.WriteLocked {
					state = "external_locked"
				}
				owners[key] = OwnershipMetadata{
					State: state, SourceSystemID: ownership.SourceSystemID, SourceRecordID: ownership.SourceRecordID, ClaimedAt: ownership.ClaimedAt,
				}
				item.Provenance = Provenance{SourceSystemID: ownership.SourceSystemID, SourceRecordID: ownership.SourceRecordID}
			case errors.Is(err, guard.ErrNotFound):
				owners[key] = OwnershipMetadata{State: "local"}
			default:
				return nil, nil, fmt.Errorf("read Exchange ownership: %w", err)
			}
			records[key] = item
			if len(records) > MaximumRecords {
				return nil, nil, ErrTooLarge
			}
		}
	}
	return records, owners, nil
}

func (s *Service) validateSchema(ctx context.Context, record Record, missing []Reference, allowHolding bool) (patterns.ValidationResult, []byte, error) {
	// Imports honor the exact immutable version pinned in the archive. A newer
	// custom version may exist by import time; that must not invalidate an older
	// package whose exact schema remains active and retrievable.
	template, err := s.schemas.GetTemplate(ctx, record.TemplateID, record.TemplateVersion)
	if err != nil || template.ID != record.TemplateID || template.Version != record.TemplateVersion || template.RecordType != record.Type || template.Status != patterns.StatusActive {
		return patterns.ValidationResult{}, nil, errors.Join(ErrInvalidInput, err)
	}
	decoder := json.NewDecoder(bytes.NewReader(record.Payload))
	decoder.UseNumber()
	var values map[string]any
	if err := decoder.Decode(&values); err != nil || values == nil {
		return patterns.ValidationResult{}, nil, ErrInvalidInput
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return patterns.ValidationResult{}, nil, ErrInvalidInput
	}
	missingFields := make([]string, 0, len(missing))
	for _, field := range template.Fields {
		if field.Type != patterns.FieldReference && field.Type != patterns.FieldAttachment {
			continue
		}
		value, ok := values[field.Key].(string)
		if !ok || value == "" {
			continue
		}
		for _, dependency := range missing {
			if dependency.ID == value && (field.ReferenceType == "stewardmesh.record" || canonicalSchemaRecordType(field.ReferenceType) == canonicalSchemaRecordType(dependency.Type)) {
				missingFields = append(missingFields, field.Key)
				break
			}
		}
	}
	result, err := s.schemas.Validate(ctx, record.TemplateID, record.TemplateVersion, patterns.ValidationInput{
		Values: values, MissingReferences: missingFields, AllowHoldingRecord: allowHolding,
	})
	if err != nil || result.Status == patterns.ValidationInvalid || result.Status != patterns.ValidationValid && result.Status != patterns.ValidationHolding || len(missingFields) > 0 && result.Status != patterns.ValidationHolding {
		return result, nil, errors.Join(ErrInvalidInput, err)
	}
	normalized, err := json.Marshal(result.NormalizedValues)
	if err != nil || len(normalized) == 0 || len(normalized) > MaximumPayloadBytes {
		return result, nil, ErrInvalidInput
	}
	return result, normalized, nil
}

func canonicalSchemaRecordType(value string) string {
	return strings.ReplaceAll(value, "_", "-")
}

func holdingReferences(values []patterns.HoldingReference, missing []Reference) []Reference {
	result := make([]Reference, 0, len(values))
	for _, value := range values {
		recordType := canonicalSchemaRecordType(strings.TrimSpace(value.ReferenceType))
		id := strings.TrimSpace(value.Value)
		for _, dependency := range missing {
			if dependency.ID == id && (recordType == "stewardmesh.record" || recordType == canonicalSchemaRecordType(dependency.Type)) {
				// Preserve the exact manifest spelling. Providers own their canonical
				// dependency type (for example ledger.purchase_order), while Patterns
				// uses a portable hyphenated record type.
				result = append(result, dependency)
				break
			}
		}
	}
	return result
}

func schemaReferences(records []Record) []SchemaReference {
	byType := make(map[string]SchemaReference)
	for _, record := range records {
		byType[record.Type] = SchemaReference{RecordType: record.Type, TemplateID: record.TemplateID, TemplateVersion: record.TemplateVersion}
	}
	types := make([]string, 0, len(byType))
	for recordType := range byType {
		types = append(types, recordType)
	}
	sort.Strings(types)
	result := make([]SchemaReference, 0, len(types))
	for _, recordType := range types {
		result = append(result, byType[recordType])
	}
	return result
}

func topologicalRecords(records map[string]Record) ([]Record, error) {
	visiting := make(map[string]bool, len(records))
	visited := make(map[string]bool, len(records))
	result := make([]Record, 0, len(records))
	var visit func(string) error
	visit = func(key string) error {
		if visiting[key] {
			return fmt.Errorf("%w: dependency cycle", ErrInvalidInput)
		}
		if visited[key] {
			return nil
		}
		visiting[key] = true
		record := records[key]
		for _, dependency := range record.Dependencies {
			if _, included := records[dependency.Key()]; included {
				if err := visit(dependency.Key()); err != nil {
					return err
				}
			}
		}
		visiting[key] = false
		visited[key] = true
		result = append(result, cloneRecord(record))
		return nil
	}
	for _, key := range sortedRecordKeys(records) {
		if err := visit(key); err != nil {
			return nil, err
		}
	}
	return result, nil
}

func normalizeReferences(values []Reference) []Reference {
	byKey := make(map[string]Reference, len(values))
	for _, value := range values {
		byKey[value.Key()] = value
	}
	keys := make([]string, 0, len(byKey))
	for key := range byKey {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	result := make([]Reference, 0, len(keys))
	for _, key := range keys {
		result = append(result, byKey[key])
	}
	return result
}

func sortedRecordKeys(records map[string]Record) []string {
	keys := make([]string, 0, len(records))
	for key := range records {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func cloneRecord(value Record) Record {
	value.Dependencies = append([]Reference(nil), value.Dependencies...)
	value.Payload = append([]byte(nil), value.Payload...)
	if value.Ownership.ClaimedAt != nil {
		claimedAt := *value.Ownership.ClaimedAt
		value.Ownership.ClaimedAt = &claimedAt
	}
	if value.File != nil {
		file := *value.File
		value.File = &file
	}
	return value
}

func (s *Service) audit(ctx context.Context, actorID, action string, value Package) error {
	scope, ok := foundation.ScopeFromContext(ctx)
	if !ok || scope.CorrelationID == "" {
		correlationID, err := foundation.NewCorrelationID()
		if err != nil {
			return err
		}
		scope = foundation.Scope{OrganizationID: s.organizationID, ActorID: actorID, CorrelationID: correlationID}
		ctx = foundation.WithScope(ctx, scope)
	}
	eventID, err := foundation.NewCorrelationID()
	if err != nil {
		return err
	}
	metadata := map[string]string{
		"requirementId": RequirementID, "featureId": FeatureID, "direction": string(value.Direction),
		"schemaVersion": value.SchemaVersion, "status": string(value.Status), "archiveSha256": value.ArchiveSHA256,
		"recordCount": fmt.Sprint(value.RecordCount), "fileCount": fmt.Sprint(value.FileCount),
		"createdCount": fmt.Sprint(value.CreatedCount), "unchangedCount": fmt.Sprint(value.UnchangedCount),
		"holdingCount": fmt.Sprint(value.HoldingCount),
	}
	return s.auditor.Record(ctx, foundation.AuditEvent{
		ID: eventID, OrganizationID: s.organizationID, ActorID: actorID, CorrelationID: scope.CorrelationID,
		Action: action, ResourceType: "exchange.package", ResourceID: value.PackageID,
		OccurredAt: normalizedNow(s.now()), Metadata: metadata,
	})
}

func nextUpdatedAt(now, previous time.Time) time.Time {
	now = normalizedNow(now)
	if !now.After(previous) {
		return previous.Add(time.Microsecond)
	}
	return now
}
