// Package exchange implements bounded, dependency-aware StewardMesh migration
// packages. Requirements: REQ-EXCHANGE-001, REQ-PATTERNS-001. Features: migration.packages, templates.schemas.
package exchange

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/maxlemke/stewardmesh/internal/patterns"
)

const (
	RequirementID = "REQ-EXCHANGE-001"
	FeatureID     = "migration.packages"
	// LegacySchemaVersion remains readable in durable receipt history only.
	// Archive decoding and every newly created service workflow require 1.1.
	LegacySchemaVersion = "1.0"
	SchemaVersion       = "1.1"
	MediaType           = "application/vnd.stewardmesh.openinventory+zip"

	MaximumArchiveBytes        = int64(32 << 20)
	MaximumUncompressedBytes   = int64(64 << 20)
	MaximumManifestBytes       = int64(32 << 20)
	MaximumFileBytes           = int64(16 << 20)
	MaximumPayloadBytes        = 1 << 20
	MaximumPayloadTotalBytes   = 24 << 20
	MaximumRecords             = 10_000
	MaximumSelections          = 1_000
	MaximumDependencies        = 128
	MaximumOutcomeDependencies = MaximumDependencies + 2
	MaximumFiles               = 1_000
	MaximumHistory             = 100
	// ProcessingLease bounds how long a crashed importer can strand a receipt.
	// Every durable progress checkpoint renews the lease through UpdatedAt.
	ProcessingLease = 5 * time.Minute
)

var (
	ErrInvalidInput      = errors.New("invalid Exchange input")
	ErrNotFound          = errors.New("Exchange record not found")
	ErrConflict          = errors.New("Exchange package conflicts with existing data")
	ErrTooLarge          = errors.New("Exchange package exceeds a configured limit")
	ErrIntegrity         = errors.New("Exchange package integrity verification failed")
	ErrDependencyMissing = errors.New("Exchange dependency is missing")
)

type FileMode string

const (
	FileModeMetadata FileMode = "metadata"
	FileModeInclude  FileMode = "include"
)

type PackageDirection string

const (
	DirectionExport PackageDirection = "export"
	DirectionImport PackageDirection = "import"
)

type PackageStatus string

const (
	StatusProcessing PackageStatus = "processing"
	StatusCompleted  PackageStatus = "completed"
	StatusHolding    PackageStatus = "holding"
	StatusFailed     PackageStatus = "failed"
)

type OutcomeStatus string

const (
	OutcomeCreated   OutcomeStatus = "created"
	OutcomeUnchanged OutcomeStatus = "unchanged"
	OutcomeHolding   OutcomeStatus = "holding"
)

// Reference is a stable provider-neutral relationship identity.
type Reference struct {
	Type string `json:"type"`
	ID   string `json:"id"`
}

func (r Reference) Key() string { return r.Type + ":" + r.ID }

// SchemaReference pins the exact immutable Patterns contract used for a
// record family. It is repeated in the manifest registry and each record so a
// consumer never guesses from the record type or a local latest version.
type SchemaReference struct {
	RecordType      string `json:"recordType"`
	TemplateID      string `json:"templateId"`
	TemplateVersion int64  `json:"templateVersion"`
}

// Provenance preserves the earliest known source identity rather than
// replacing it with a transport filename or an object-store URL.
type Provenance struct {
	SourceSystemID string `json:"sourceSystemId"`
	SourceRecordID string `json:"sourceRecordId"`
}

// OwnershipMetadata intentionally excludes account identifiers. The package
// needs ownership state and source identity, not operator PII.
type OwnershipMetadata struct {
	State          string     `json:"state"`
	SourceSystemID string     `json:"sourceSystemId,omitempty"`
	SourceRecordID string     `json:"sourceRecordId,omitempty"`
	ClaimedAt      *time.Time `json:"claimedAt,omitempty"`
}

// FileMetadata never carries an object key, credential, download token, or
// signed URL. Entry is a package-internal fixed path only when bytes are
// included.
type FileMetadata struct {
	Mode      FileMode `json:"mode"`
	Name      string   `json:"name"`
	MediaType string   `json:"mediaType"`
	SizeBytes int64    `json:"sizeBytes"`
	SHA256    string   `json:"sha256"`
	Entry     string   `json:"entry,omitempty"`
}

// Record is the portable domain boundary. Payload remains typed JSON owned by
// its provider; Exchange verifies identity, bounds, dependencies, and checksum.
type Record struct {
	Type            string            `json:"type"`
	ID              string            `json:"id"`
	Revision        int64             `json:"revision"`
	TemplateID      string            `json:"templateId"`
	TemplateVersion int64             `json:"templateVersion"`
	Checksum        string            `json:"checksum"`
	Dependencies    []Reference       `json:"dependencies"`
	Provenance      Provenance        `json:"provenance"`
	Ownership       OwnershipMetadata `json:"ownership"`
	File            *FileMetadata     `json:"file,omitempty"`
	Payload         json.RawMessage   `json:"payload"`
}

type RecordDescriptor struct {
	Type            string      `json:"type"`
	ID              string      `json:"id"`
	Revision        int64       `json:"revision"`
	TemplateID      string      `json:"templateId"`
	TemplateVersion int64       `json:"templateVersion"`
	Dependencies    []Reference `json:"dependencies"`
	HasFile         bool        `json:"hasFile"`
}

type Manifest struct {
	SchemaVersion  string            `json:"schemaVersion"`
	PackageID      string            `json:"packageId"`
	SourceSystemID string            `json:"sourceSystemId"`
	ExportedAt     time.Time         `json:"exportedAt"`
	FileMode       FileMode          `json:"fileMode"`
	Schemas        []SchemaReference `json:"schemas"`
	Records        []Record          `json:"records"`
}

type ExportRequest struct {
	Selection           []Reference `json:"selection"`
	IncludeDependencies bool        `json:"includeDependencies"`
	FileMode            FileMode    `json:"fileMode"`
}

type ExportArtifact struct {
	PackageID string
	SHA256    string
	Bytes     []byte
}

type RecordOutcome struct {
	Type                string        `json:"type"`
	ID                  string        `json:"id"`
	Revision            int64         `json:"revision"`
	Checksum            string        `json:"checksum"`
	Status              OutcomeStatus `json:"status"`
	MissingDependencies []Reference   `json:"missingDependencies"`
	WriteLocked         bool          `json:"writeLocked"`
}

const (
	progressIntent    = "intent"
	progressCommitted = "committed"
)

// ImportProgress is private durable recovery state. It is persisted with the
// receipt but never serialized through REST or gRPC. The operation token is the
// provider idempotency/fencing identity; ExpectedCreated preserves truthful
// outcome attribution across a crash after a domain commit.
type ImportProgress struct {
	Type             string `json:"type"`
	ID               string `json:"id"`
	Checksum         string `json:"checksum"`
	OperationToken   string `json:"operationToken"`
	Phase            string `json:"phase"`
	ExpectedCreated  bool   `json:"expectedCreated"`
	OwnershipReady   bool   `json:"ownershipReady"`
	OwnershipCreated bool   `json:"ownershipCreated"`
	WriteLocked      bool   `json:"writeLocked"`
}

func (p ImportProgress) key() string { return Reference{Type: p.Type, ID: p.ID}.Key() }

type Package struct {
	OrganizationID string           `json:"-"`
	PackageID      string           `json:"packageId"`
	Direction      PackageDirection `json:"direction"`
	SchemaVersion  string           `json:"schemaVersion"`
	SourceSystemID string           `json:"sourceSystemId"`
	ArchiveSHA256  string           `json:"archiveSha256"`
	SizeBytes      int64            `json:"sizeBytes"`
	FileMode       FileMode         `json:"fileMode"`
	Status         PackageStatus    `json:"status"`
	RecordCount    int              `json:"recordCount"`
	FileCount      int              `json:"fileCount"`
	CreatedCount   int              `json:"createdCount"`
	UnchangedCount int              `json:"unchangedCount"`
	HoldingCount   int              `json:"holdingCount"`
	Records        []RecordOutcome  `json:"records"`
	Progress       []ImportProgress `json:"-"`
	ErrorCode      string           `json:"errorCode,omitempty"`
	CreatedBy      string           `json:"-"`
	CreatedAt      time.Time        `json:"createdAt"`
	UpdatedAt      time.Time        `json:"updatedAt"`
}

func (p Package) Validate() error {
	if !stableIDPattern.MatchString(p.OrganizationID) || !stableIDPattern.MatchString(p.PackageID) ||
		!stableIDPattern.MatchString(p.SourceSystemID) || p.SchemaVersion != LegacySchemaVersion && p.SchemaVersion != SchemaVersion || !sha256Pattern.MatchString(p.ArchiveSHA256) ||
		(p.Direction != DirectionExport && p.Direction != DirectionImport) ||
		(p.FileMode != FileModeMetadata && p.FileMode != FileModeInclude) ||
		(p.Status != StatusProcessing && p.Status != StatusCompleted && p.Status != StatusHolding && p.Status != StatusFailed) ||
		p.SizeBytes <= 0 || p.SizeBytes > MaximumArchiveBytes || p.RecordCount < 1 || p.RecordCount > MaximumRecords ||
		p.FileCount < 0 || p.FileCount > MaximumFiles || p.FileCount > p.RecordCount || p.CreatedCount < 0 || p.UnchangedCount < 0 || p.HoldingCount < 0 ||
		p.CreatedCount+p.UnchangedCount+p.HoldingCount > p.RecordCount || p.CreatedAt.IsZero() || p.UpdatedAt.Before(p.CreatedAt) ||
		strings.TrimSpace(p.CreatedBy) == "" || !utf8.ValidString(p.CreatedBy) || utf8.RuneCountInString(p.CreatedBy) > 128 {
		return ErrInvalidInput
	}
	if (p.Status == StatusCompleted || p.Status == StatusHolding) && len(p.Records) != p.RecordCount {
		return ErrInvalidInput
	}
	if len(p.Records) > MaximumRecords || (p.Status == StatusHolding) != (p.HoldingCount > 0) ||
		(p.Status == StatusCompleted && p.HoldingCount != 0) || (p.Status == StatusFailed) != (p.ErrorCode != "") {
		return ErrInvalidInput
	}
	if p.ErrorCode != "" && !errorCodePattern.MatchString(p.ErrorCode) {
		return ErrInvalidInput
	}
	if (p.Status == StatusProcessing || p.Status == StatusFailed) && p.HoldingCount != 0 {
		return ErrInvalidInput
	}
	if p.Direction == DirectionExport && (p.Status != StatusCompleted || p.CreatedCount != 0 || p.HoldingCount != 0 || p.UnchangedCount != p.RecordCount) {
		return ErrInvalidInput
	}
	counts := map[OutcomeStatus]int{OutcomeCreated: 0, OutcomeUnchanged: 0, OutcomeHolding: 0}
	seen := make(map[string]struct{}, len(p.Records))
	outcomes := make(map[string]RecordOutcome, len(p.Records))
	for _, record := range p.Records {
		if !resourceTypePattern.MatchString(record.Type) || !stableIDPattern.MatchString(record.ID) || record.Revision < 1 ||
			!sha256Pattern.MatchString(record.Checksum) ||
			(record.Status != OutcomeCreated && record.Status != OutcomeUnchanged && record.Status != OutcomeHolding) ||
			len(record.MissingDependencies) > MaximumOutcomeDependencies {
			return ErrInvalidInput
		}
		key := record.Type + "\x00" + record.ID
		if _, duplicate := seen[key]; duplicate {
			return ErrInvalidInput
		}
		seen[key] = struct{}{}
		outcomes[Reference{Type: record.Type, ID: record.ID}.Key()] = record
		counts[record.Status]++
		if record.Status == OutcomeHolding && (record.WriteLocked || len(record.MissingDependencies) == 0) {
			return ErrInvalidInput
		}
		if record.Status != OutcomeHolding && len(record.MissingDependencies) != 0 {
			return ErrInvalidInput
		}
		previous := ""
		for _, dependency := range record.MissingDependencies {
			if !resourceTypePattern.MatchString(dependency.Type) || !stableIDPattern.MatchString(dependency.ID) || dependency.Key() <= previous {
				return ErrInvalidInput
			}
			previous = dependency.Key()
		}
	}
	if len(p.Progress) > p.RecordCount || (p.Status == StatusCompleted || p.Status == StatusHolding || p.Direction == DirectionExport) && len(p.Progress) != 0 {
		return ErrInvalidInput
	}
	progressSeen := make(map[string]struct{}, len(p.Progress))
	for _, progress := range p.Progress {
		if !resourceTypePattern.MatchString(progress.Type) || !stableIDPattern.MatchString(progress.ID) ||
			!sha256Pattern.MatchString(progress.Checksum) || !stableIDPattern.MatchString(progress.OperationToken) ||
			(progress.Phase != progressIntent && progress.Phase != progressCommitted) || (!progress.OwnershipReady && (progress.OwnershipCreated || progress.WriteLocked)) {
			return ErrInvalidInput
		}
		if _, duplicate := progressSeen[progress.key()]; duplicate {
			return ErrInvalidInput
		}
		progressSeen[progress.key()] = struct{}{}
		if progress.Phase == progressCommitted {
			if !progress.OwnershipReady {
				return ErrInvalidInput
			}
			outcome, ok := outcomes[progress.key()]
			if !ok || outcome.Status == OutcomeHolding || outcome.Checksum != progress.Checksum || outcome.WriteLocked != progress.WriteLocked {
				return ErrInvalidInput
			}
			if (progress.ExpectedCreated && outcome.Status != OutcomeCreated) || (!progress.ExpectedCreated && outcome.Status != OutcomeUnchanged) {
				return ErrInvalidInput
			}
		} else if _, hasOutcome := outcomes[progress.key()]; hasOutcome {
			return ErrInvalidInput
		}
	}
	if counts[OutcomeCreated] != p.CreatedCount || counts[OutcomeUnchanged] != p.UnchangedCount || counts[OutcomeHolding] != p.HoldingCount {
		return ErrInvalidInput
	}
	return nil
}

// ValidateTransitionFrom protects receipt progress from rollback. Processing
// checkpoints may only add successful outcomes, terminal states must retain
// them, and retries keep the successful subset while re-evaluating holdings.
func (p Package) ValidateTransitionFrom(previous Package) error {
	if err := p.Validate(); err != nil {
		return err
	}
	switch previous.Status {
	case StatusProcessing:
		if p.Status != StatusProcessing && p.Status != StatusCompleted && p.Status != StatusHolding && p.Status != StatusFailed {
			return ErrConflict
		}
		if !containsSuccessfulOutcomes(p.Records, previous.Records) {
			return ErrConflict
		}
		if !validProgressTransition(p, previous, false) {
			return ErrConflict
		}
		if !successfulOutcomesHaveRecoveryState(p, previous) {
			return ErrConflict
		}
	case StatusHolding, StatusFailed:
		if p.Status != StatusProcessing || !sameOutcomes(p.Records, successfulOutcomes(previous.Records)) {
			return ErrConflict
		}
		if !validProgressTransition(p, previous, true) {
			return ErrConflict
		}
	default:
		return ErrConflict
	}
	return nil
}

func successfulOutcomesHaveRecoveryState(next, previous Package) bool {
	previousOutcomes := make(map[string]struct{}, len(previous.Records))
	for _, outcome := range previous.Records {
		if outcome.Status != OutcomeHolding {
			previousOutcomes[Reference{Type: outcome.Type, ID: outcome.ID}.Key()] = struct{}{}
		}
	}
	nextProgress := make(map[string]ImportProgress, len(next.Progress))
	for _, progress := range next.Progress {
		nextProgress[progress.key()] = progress
	}
	for _, outcome := range next.Records {
		if outcome.Status == OutcomeHolding {
			continue
		}
		key := Reference{Type: outcome.Type, ID: outcome.ID}.Key()
		if _, alreadyDurable := previousOutcomes[key]; alreadyDurable {
			continue
		}
		progress, ok := nextProgress[key]
		if !ok || progress.Phase != progressCommitted || progress.Checksum != outcome.Checksum {
			return false
		}
	}
	return true
}

func validProgressTransition(next, previous Package, retry bool) bool {
	nextByKey := make(map[string]ImportProgress, len(next.Progress))
	for _, progress := range next.Progress {
		nextByKey[progress.key()] = progress
	}
	previousByKey := make(map[string]ImportProgress, len(previous.Progress))
	for _, progress := range previous.Progress {
		previousByKey[progress.key()] = progress
	}
	for key, before := range previousByKey {
		after, ok := nextByKey[key]
		if !ok {
			if next.Status == StatusCompleted || next.Status == StatusHolding {
				continue
			}
			if before.Phase == progressCommitted {
				outcome, durable := successfulOutcomeFor(next.Records, key)
				if durable && outcome.Checksum == before.Checksum {
					continue
				}
			}
			if before.Phase == progressIntent && (next.Status == StatusFailed || next.Status == StatusProcessing) {
				continue
			}
			return false
		}
		if before.Type != after.Type || before.ID != after.ID || before.Checksum != after.Checksum ||
			before.OperationToken != after.OperationToken || before.ExpectedCreated != after.ExpectedCreated ||
			(before.OwnershipReady && !after.OwnershipReady) || (before.OwnershipCreated && !after.OwnershipCreated) ||
			(before.WriteLocked && !after.WriteLocked) || before.Phase == progressCommitted && after.Phase != progressCommitted {
			return false
		}
		if retry && before != after {
			return false
		}
	}
	if retry && len(nextByKey) != len(previousByKey) {
		return false
	}
	for key := range nextByKey {
		if _, existed := previousByKey[key]; !existed && next.Status != StatusProcessing {
			return false
		}
	}
	return true
}

func successfulOutcomeFor(values []RecordOutcome, key string) (RecordOutcome, bool) {
	for _, outcome := range values {
		if (Reference{Type: outcome.Type, ID: outcome.ID}).Key() == key && outcome.Status != OutcomeHolding {
			return outcome, true
		}
	}
	return RecordOutcome{}, false
}

func containsSuccessfulOutcomes(next, previous []RecordOutcome) bool {
	byKey := make(map[string]RecordOutcome, len(next))
	for _, outcome := range next {
		byKey[Reference{Type: outcome.Type, ID: outcome.ID}.Key()] = outcome
	}
	for _, outcome := range previous {
		if outcome.Status == OutcomeHolding {
			continue
		}
		candidate, ok := byKey[Reference{Type: outcome.Type, ID: outcome.ID}.Key()]
		if !ok || !sameOutcome(candidate, outcome) {
			return false
		}
	}
	return true
}

func successfulOutcomes(values []RecordOutcome) []RecordOutcome {
	result := make([]RecordOutcome, 0, len(values))
	for _, outcome := range values {
		if outcome.Status != OutcomeHolding {
			result = append(result, cloneOutcome(outcome))
		}
	}
	return result
}

func sameOutcomes(left, right []RecordOutcome) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if !sameOutcome(left[index], right[index]) {
			return false
		}
	}
	return true
}

func sameOutcome(left, right RecordOutcome) bool {
	if left.Type != right.Type || left.ID != right.ID || left.Revision != right.Revision || left.Checksum != right.Checksum ||
		left.Status != right.Status || left.WriteLocked != right.WriteLocked || len(left.MissingDependencies) != len(right.MissingDependencies) {
		return false
	}
	for index := range left.MissingDependencies {
		if left.MissingDependencies[index] != right.MissingDependencies[index] {
			return false
		}
	}
	return true
}

func cloneOutcome(value RecordOutcome) RecordOutcome {
	value.MissingDependencies = append([]Reference(nil), value.MissingDependencies...)
	return value
}

type ImportResult struct {
	Package Package `json:"package"`
	Replay  bool    `json:"replay"`
}

type ProviderImportOperation struct {
	Token           string
	Repair          bool
	ExpectedCreated bool
	OccurredAt      time.Time
}

type ProviderImportResult struct {
	// Committed means the exact domain record is durable even if Err reports a
	// post-commit audit failure. Exchange must retain Guard ownership and expose
	// the truthful created/unchanged outcome before retrying repair.
	Committed bool
	Created   bool
}

// Provider keeps Exchange independent from PostgreSQL, HTTP, and concrete
// domain schemas. Providers must use their domain service for import.
type Provider interface {
	Types() []string
	ListRecords(ctx context.Context) ([]Record, error)
	Exists(ctx context.Context, reference Reference) (bool, error)
	// ImportRecordExists performs an exact, read-only provider comparison
	// before an intent is reserved. It never audits or mutates a target.
	ImportRecordExists(ctx context.Context, record Record, file []byte) (bool, error)
	ImportRecord(ctx context.Context, operation ProviderImportOperation, sourceSystemID string, record Record, file []byte) (ProviderImportResult, error)
}

// SchemaRegistry is the Patterns seam used before archive export and before
// any provider or ownership mutation during import.
type SchemaRegistry interface {
	ActiveTemplateForRecordType(context.Context, string) (patterns.Template, error)
	GetTemplate(context.Context, string, int64) (patterns.Template, error)
	Validate(context.Context, string, int64, patterns.ValidationInput) (patterns.ValidationResult, error)
}

type FileReader interface {
	ReadRecordFile(ctx context.Context, record Record) ([]byte, error)
}

// MetadataOnlyResolver lets a file-backed provider prove that the exact target
// already exists without requiring package bytes. A false result is held as a
// missing exchange.file dependency; implementations must never create data.
type MetadataOnlyResolver interface {
	MetadataOnlyRecordExists(ctx context.Context, record Record) (bool, error)
}

// DependencyResolver lets a provider verify relationships owned by another
// domain without pretending it can export or import that domain's records.
// This keeps cross-domain references visible while avoiding generic writes.
type DependencyResolver interface {
	DependencyExists(ctx context.Context, reference Reference) (handled bool, exists bool, err error)
}

type Store interface {
	ListPackages(ctx context.Context, organizationID string, limit int) ([]Package, error)
	GetPackage(ctx context.Context, organizationID string, direction PackageDirection, packageID string) (Package, error)
	CreatePackage(ctx context.Context, value Package) (Package, bool, error)
	UpdatePackage(ctx context.Context, value Package, expectedUpdatedAt time.Time) (Package, error)
}
