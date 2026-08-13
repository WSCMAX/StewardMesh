// Package exchange implements bounded, dependency-aware StewardMesh migration
// packages. Requirement: REQ-EXCHANGE-001. Feature: migration.packages.
package exchange

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	RequirementID = "REQ-EXCHANGE-001"
	FeatureID     = "migration.packages"
	SchemaVersion = "1.0"
	MediaType     = "application/vnd.stewardmesh.openinventory+zip"

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
	Type         string            `json:"type"`
	ID           string            `json:"id"`
	Revision     int64             `json:"revision"`
	Checksum     string            `json:"checksum"`
	Dependencies []Reference       `json:"dependencies"`
	Provenance   Provenance        `json:"provenance"`
	Ownership    OwnershipMetadata `json:"ownership"`
	File         *FileMetadata     `json:"file,omitempty"`
	Payload      json.RawMessage   `json:"payload"`
}

type RecordDescriptor struct {
	Type         string      `json:"type"`
	ID           string      `json:"id"`
	Revision     int64       `json:"revision"`
	Dependencies []Reference `json:"dependencies"`
	HasFile      bool        `json:"hasFile"`
}

type Manifest struct {
	SchemaVersion  string    `json:"schemaVersion"`
	PackageID      string    `json:"packageId"`
	SourceSystemID string    `json:"sourceSystemId"`
	ExportedAt     time.Time `json:"exportedAt"`
	FileMode       FileMode  `json:"fileMode"`
	Records        []Record  `json:"records"`
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
	ErrorCode      string           `json:"errorCode,omitempty"`
	CreatedBy      string           `json:"-"`
	CreatedAt      time.Time        `json:"createdAt"`
	UpdatedAt      time.Time        `json:"updatedAt"`
}

func (p Package) Validate() error {
	if !stableIDPattern.MatchString(p.OrganizationID) || !stableIDPattern.MatchString(p.PackageID) ||
		!stableIDPattern.MatchString(p.SourceSystemID) || p.SchemaVersion != SchemaVersion || !sha256Pattern.MatchString(p.ArchiveSHA256) ||
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
	if p.Status == StatusProcessing && (len(p.Records) != 0 || p.CreatedCount != 0 || p.UnchangedCount != 0 || p.HoldingCount != 0) {
		return ErrInvalidInput
	}
	if p.Status == StatusFailed && (len(p.Records) != 0 || p.CreatedCount != 0 || p.UnchangedCount != 0 || p.HoldingCount != 0) {
		return ErrInvalidInput
	}
	if p.Direction == DirectionExport && (p.Status != StatusCompleted || p.CreatedCount != 0 || p.HoldingCount != 0 || p.UnchangedCount != p.RecordCount) {
		return ErrInvalidInput
	}
	counts := map[OutcomeStatus]int{OutcomeCreated: 0, OutcomeUnchanged: 0, OutcomeHolding: 0}
	seen := make(map[string]struct{}, len(p.Records))
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
	if (p.Status == StatusCompleted || p.Status == StatusHolding) &&
		(counts[OutcomeCreated] != p.CreatedCount || counts[OutcomeUnchanged] != p.UnchangedCount || counts[OutcomeHolding] != p.HoldingCount) {
		return ErrInvalidInput
	}
	return nil
}

type ImportResult struct {
	Package Package `json:"package"`
	Replay  bool    `json:"replay"`
}

// Provider keeps Exchange independent from PostgreSQL, HTTP, and concrete
// domain schemas. Providers must use their domain service for import.
type Provider interface {
	Types() []string
	ListRecords(ctx context.Context) ([]Record, error)
	Exists(ctx context.Context, reference Reference) (bool, error)
	ImportRecord(ctx context.Context, sourceSystemID string, record Record, file []byte) (created bool, err error)
}

type FileReader interface {
	ReadRecordFile(ctx context.Context, record Record) ([]byte, error)
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
