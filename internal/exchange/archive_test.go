package exchange

// Requirements: REQ-EXCHANGE-001, REQ-PATTERNS-001. Features: migration.packages, templates.schemas.
// Security regression coverage for bounded
// archive parsing, checksums, and credential-free payloads.

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

func TestArchiveRoundTripVerifiesRecordsAndIncludedFiles(t *testing.T) {
	content := []byte("verified evidence")
	digest := sha256.Sum256(content)
	record := validArchiveRecord()
	record.Type = "vault.blob"
	record.ID = "0123456789abcdef0123456789abcdef"
	record.TemplateID = "builtin-vault-blob"
	record.File = &FileMetadata{
		Mode: FileModeMetadata, Name: "evidence.txt", MediaType: "text/plain",
		SizeBytes: int64(len(content)), SHA256: hex.EncodeToString(digest[:]),
	}
	manifest := validArchiveManifest([]Record{record}, FileModeInclude)
	artifact, sealed, err := encodeArchive(manifest, map[string][]byte{Reference{Type: record.Type, ID: record.ID}.Key(): content})
	if err != nil {
		t.Fatal(err)
	}
	if len(artifact.Bytes) == 0 || !sha256Pattern.MatchString(artifact.SHA256) || sealed.Records[0].File.Entry == "" {
		t.Fatalf("unexpected Exchange artifact %#v manifest=%#v", artifact, sealed)
	}
	decoded, checksum, err := decodeArchive(artifact.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	if checksum != artifact.SHA256 || decoded.Manifest.Records[0].Checksum == "" || !bytes.Equal(decoded.Files[sealed.Records[0].File.Entry], content) {
		t.Fatalf("archive did not round trip: %#v checksum=%q", decoded.Manifest, checksum)
	}
}

func TestServiceRejectsLegacyOnePointZeroArchiveBeforeAnyWorkflowMutation(t *testing.T) {
	_, sealed, err := encodeArchive(validArchiveManifest([]Record{validArchiveRecord()}, FileModeMetadata), nil)
	if err != nil {
		t.Fatal(err)
	}
	sealed.SchemaVersion = LegacySchemaVersion
	manifest, err := json.Marshal(sealed)
	if err != nil {
		t.Fatal(err)
	}
	legacy := rawZip(t, map[string][]byte{manifestEntry: manifest})
	// Every dependency is deliberately nil. Reaching receipt, Patterns, Guard,
	// or provider work would panic; schema rejection must happen first.
	service := &Service{}
	if _, err := service.Import(context.Background(), "operator", legacy); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("expected legacy archive rejection before mutation, got %v", err)
	}
}

func TestArchiveRejectsCorruptionDuplicateRecordsAndUnsafeEntries(t *testing.T) {
	record := validArchiveRecord()
	artifact, sealed, err := encodeArchive(validArchiveManifest([]Record{record}, FileModeMetadata), nil)
	if err != nil {
		t.Fatal(err)
	}

	t.Run("record checksum mismatch", func(t *testing.T) {
		sealed.Records[0].Payload = json.RawMessage(`{"name":"tampered"}`)
		manifest, _ := json.Marshal(sealed)
		_, _, err := decodeArchive(rawZip(t, map[string][]byte{manifestEntry: manifest}))
		if !errors.Is(err, ErrIntegrity) {
			t.Fatalf("expected integrity error, got %v", err)
		}
	})

	t.Run("corrupt zip", func(t *testing.T) {
		corrupt := append([]byte(nil), artifact.Bytes...)
		corrupt[len(corrupt)/2] ^= 0xff
		if _, _, err := decodeArchive(corrupt); !errors.Is(err, ErrIntegrity) {
			t.Fatalf("expected corrupt archive rejection, got %v", err)
		}
	})

	t.Run("duplicate record identity", func(t *testing.T) {
		_, _, err := encodeArchive(validArchiveManifest([]Record{record, record}, FileModeMetadata), nil)
		if !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("expected duplicate rejection, got %v", err)
		}
	})

	t.Run("path traversal entry", func(t *testing.T) {
		if _, _, err := decodeArchive(rawZip(t, map[string][]byte{"../manifest.json": []byte(`{}`)})); !errors.Is(err, ErrIntegrity) {
			t.Fatalf("expected unsafe path rejection, got %v", err)
		}
	})

	t.Run("duplicate ZIP entry", func(t *testing.T) {
		manifest, _ := json.Marshal(sealed)
		if _, _, err := decodeArchive(rawZipEntries(t, []zipTestEntry{
			{name: manifestEntry, value: manifest}, {name: manifestEntry, value: manifest},
		})); !errors.Is(err, ErrIntegrity) {
			t.Fatalf("expected duplicate ZIP entry rejection, got %v", err)
		}
	})

	t.Run("unreferenced file", func(t *testing.T) {
		manifest, _ := json.Marshal(sealed)
		entries := map[string][]byte{manifestEntry: manifest, "files/" + string(make([]byte, 64)): []byte("x")}
		if _, _, err := decodeArchive(rawZip(t, entries)); !errors.Is(err, ErrIntegrity) {
			t.Fatalf("expected unreferenced file rejection, got %v", err)
		}
	})
}

func TestArchiveRejectsSecretsSignedURLsAndResourceExhaustion(t *testing.T) {
	for name, payload := range map[string]string{
		"credential key":        `{"clientSecret":"must-not-export"}`,
		"API key":               `{"apiKey":"must-not-export"}`,
		"bearer token":          `{"token":"must-not-export"}`,
		"signed URL":            `{"download":"https://objects.example.test/file?X-Amz-Signature=abc"}`,
		"mixed-case signed URL": `{"download":"HtTpS://objects.example.test/file?X-Amz-Signature=abc"}`,
		"embedded user info":    `{"endpoint":"https://user:password@example.test/file"}`,
		"mixed-case user info":  `{"endpoint":"HTTP://user:password@example.test/file"}`,
	} {
		t.Run(name, func(t *testing.T) {
			record := validArchiveRecord()
			record.Payload = json.RawMessage(payload)
			if _, _, err := encodeArchive(validArchiveManifest([]Record{record}, FileModeMetadata), nil); !errors.Is(err, ErrInvalidInput) {
				t.Fatalf("expected unsafe payload rejection, got %v", err)
			}
		})
	}

	if _, _, err := decodeArchive(make([]byte, MaximumArchiveBytes+1)); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("expected compressed size rejection, got %v", err)
	}
	// Highly compressed untrusted entries are rejected before allocation reaches
	// the global uncompressed ceiling.
	bomb := rawZip(t, map[string][]byte{manifestEntry: make([]byte, 3<<20)})
	if _, _, err := decodeArchive(bomb); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("expected compression ratio rejection, got %v", err)
	}
}

func TestArchiveRejectsInconsistentOwnershipMetadata(t *testing.T) {
	tests := []OwnershipMetadata{
		{State: "local", SourceSystemID: "source", SourceRecordID: "record"},
		{State: "external_locked"},
		{State: "claimed", SourceSystemID: "source", SourceRecordID: "record"},
	}
	for _, ownership := range tests {
		record := validArchiveRecord()
		record.Ownership = ownership
		if _, _, err := encodeArchive(validArchiveManifest([]Record{record}, FileModeMetadata), nil); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("expected ownership metadata rejection for %#v, got %v", ownership, err)
		}
	}
}

func TestArchiveRoundTripsHighlyCompressibleIncludedFileWithoutCreatingABomb(t *testing.T) {
	content := make([]byte, 2<<20)
	digest := sha256.Sum256(content)
	record := validArchiveRecord()
	record.Type = "vault.blob"
	record.ID = "fedcba9876543210fedcba9876543210"
	record.TemplateID = "builtin-vault-blob"
	record.File = &FileMetadata{
		Mode: FileModeMetadata, Name: "zeros.bin", MediaType: "application/octet-stream",
		SizeBytes: int64(len(content)), SHA256: hex.EncodeToString(digest[:]),
	}
	artifact, _, err := encodeArchive(
		validArchiveManifest([]Record{record}, FileModeInclude),
		map[string][]byte{Reference{Type: record.Type, ID: record.ID}.Key(): content},
	)
	if err != nil {
		t.Fatal(err)
	}
	decoded, _, err := decodeArchive(artifact.Bytes)
	if err != nil || len(decoded.Files) != 1 {
		t.Fatalf("self-generated compressible file did not round trip: files=%d err=%v", len(decoded.Files), err)
	}
}

func validArchiveManifest(records []Record, mode FileMode) Manifest {
	return Manifest{
		SchemaVersion: SchemaVersion, PackageID: "package-0123456789abcdef", SourceSystemID: "source-system",
		ExportedAt: time.Date(2026, time.August, 13, 12, 0, 0, 0, time.UTC), FileMode: mode, Schemas: schemaReferences(records), Records: records,
	}
}

func validArchiveRecord() Record {
	return Record{
		Type: "stack.product", ID: "product-one", Revision: 1, TemplateID: "builtin-stack-product", TemplateVersion: 1, Dependencies: []Reference{},
		Provenance: Provenance{SourceSystemID: "source-system", SourceRecordID: "stack.product:product-one"},
		Ownership:  OwnershipMetadata{State: "local"}, Payload: json.RawMessage(`{"id":"product-one","name":"Safe product"}`),
	}
}

func rawZip(t *testing.T, entries map[string][]byte) []byte {
	t.Helper()
	values := make([]zipTestEntry, 0, len(entries))
	for name, value := range entries {
		values = append(values, zipTestEntry{name: name, value: value})
	}
	return rawZipEntries(t, values)
}

type zipTestEntry struct {
	name  string
	value []byte
}

func rawZipEntries(t *testing.T, entries []zipTestEntry) []byte {
	t.Helper()
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	for _, value := range entries {
		entry, err := writer.Create(value.name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write(value.value); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}
