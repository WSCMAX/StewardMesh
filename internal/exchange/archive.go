package exchange

// Requirements: REQ-EXCHANGE-001, REQ-PATTERNS-001. Features: migration.packages, templates.schemas. GitHub: #9, #8.

import (
	"archive/zip"
	"bytes"
	"compress/flate"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/url"
	"path"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

const manifestEntry = "manifest.json"

var (
	resourceTypePattern = regexp.MustCompile(`^[a-z][a-z0-9._-]{0,63}$`)
	stableIDPattern     = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)
	sha256Pattern       = regexp.MustCompile(`^[a-f0-9]{64}$`)
	fileEntryPattern    = regexp.MustCompile(`^files/[a-f0-9]{64}$`)
	errorCodePattern    = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)
	sensitiveKeyPattern = regexp.MustCompile(`(?i)(password|passwd|secret|credential|authorization|api[_-]?key|access[_-]?key|(^|[_-])token($|[_-])|access[_-]?token|refresh[_-]?token|session[_-]?token|private[_-]?key|signed[_-]?url)`)
)

type archiveContents struct {
	Manifest Manifest
	Files    map[string][]byte
}

type recordChecksumEnvelope struct {
	Type            string            `json:"type"`
	ID              string            `json:"id"`
	Revision        int64             `json:"revision"`
	TemplateID      string            `json:"templateId"`
	TemplateVersion int64             `json:"templateVersion"`
	Dependencies    []Reference       `json:"dependencies"`
	Provenance      Provenance        `json:"provenance"`
	Ownership       OwnershipMetadata `json:"ownership"`
	File            *FileMetadata     `json:"file,omitempty"`
	Payload         json.RawMessage   `json:"payload"`
}

func encodeArchive(manifest Manifest, filesByRecord map[string][]byte) (ExportArtifact, Manifest, error) {
	if manifest.FileMode != FileModeMetadata && manifest.FileMode != FileModeInclude {
		return ExportArtifact{}, Manifest{}, ErrInvalidInput
	}
	files := make(map[string][]byte)
	for index := range manifest.Records {
		record := &manifest.Records[index]
		if record.Dependencies == nil {
			record.Dependencies = []Reference{}
		}
		sort.Slice(record.Dependencies, func(i, j int) bool { return record.Dependencies[i].Key() < record.Dependencies[j].Key() })
		if record.File != nil {
			record.File.Mode = manifest.FileMode
			record.File.Entry = ""
			if manifest.FileMode == FileModeInclude {
				content, ok := filesByRecord[Reference{Type: record.Type, ID: record.ID}.Key()]
				if !ok || int64(len(content)) != record.File.SizeBytes || len(content) > int(MaximumFileBytes) {
					return ExportArtifact{}, Manifest{}, ErrIntegrity
				}
				digest := sha256.Sum256(content)
				checksum := hex.EncodeToString(digest[:])
				if checksum != record.File.SHA256 {
					return ExportArtifact{}, Manifest{}, ErrIntegrity
				}
				record.File.Entry = "files/" + checksum
				files[record.File.Entry] = append([]byte(nil), content...)
			}
		}
		checksum, err := checksumRecord(*record)
		if err != nil {
			return ExportArtifact{}, Manifest{}, err
		}
		record.Checksum = checksum
	}
	if err := validateManifest(manifest, files, false); err != nil {
		return ExportArtifact{}, Manifest{}, err
	}
	encodedManifest, err := json.Marshal(manifest)
	if err != nil {
		return ExportArtifact{}, Manifest{}, fmt.Errorf("encode Exchange manifest: %w", err)
	}
	if int64(len(encodedManifest)) > MaximumManifestBytes {
		return ExportArtifact{}, Manifest{}, ErrTooLarge
	}

	var buffer bytes.Buffer
	archive := zip.NewWriter(&buffer)
	write := func(name string, value []byte) error {
		header := &zip.FileHeader{Name: name, Method: safeZIPMethod(value)}
		header.SetModTime(manifest.ExportedAt.UTC())
		entry, createErr := archive.CreateHeader(header)
		if createErr != nil {
			return createErr
		}
		_, writeErr := entry.Write(value)
		return writeErr
	}
	if err := write(manifestEntry, encodedManifest); err != nil {
		_ = archive.Close()
		return ExportArtifact{}, Manifest{}, fmt.Errorf("write Exchange manifest: %w", err)
	}
	fileNames := make([]string, 0, len(files))
	for name := range files {
		fileNames = append(fileNames, name)
	}
	sort.Strings(fileNames)
	for _, name := range fileNames {
		if err := write(name, files[name]); err != nil {
			_ = archive.Close()
			return ExportArtifact{}, Manifest{}, fmt.Errorf("write Exchange file: %w", err)
		}
	}
	if err := archive.Close(); err != nil {
		return ExportArtifact{}, Manifest{}, fmt.Errorf("close Exchange package: %w", err)
	}
	if int64(buffer.Len()) > MaximumArchiveBytes {
		return ExportArtifact{}, Manifest{}, ErrTooLarge
	}
	contents := append([]byte(nil), buffer.Bytes()...)
	digest := sha256.Sum256(contents)
	return ExportArtifact{PackageID: manifest.PackageID, SHA256: hex.EncodeToString(digest[:]), Bytes: contents}, manifest, nil
}

func safeZIPMethod(value []byte) uint16 {
	if len(value) <= 1<<20 {
		return zip.Deflate
	}
	var sample bytes.Buffer
	compressor, err := flate.NewWriter(&sample, flate.BestSpeed)
	if err != nil {
		return zip.Store
	}
	_, writeErr := compressor.Write(value)
	closeErr := compressor.Close()
	if writeErr != nil || closeErr != nil || uint64(len(value)) > uint64(sample.Len())*200+(1<<20) {
		return zip.Store
	}
	return zip.Deflate
}

func decodeArchive(contents []byte) (archiveContents, string, error) {
	if len(contents) == 0 || int64(len(contents)) > MaximumArchiveBytes {
		return archiveContents{}, "", ErrTooLarge
	}
	digest := sha256.Sum256(contents)
	archiveChecksum := hex.EncodeToString(digest[:])
	reader, err := zip.NewReader(bytes.NewReader(contents), int64(len(contents)))
	if err != nil {
		return archiveContents{}, "", ErrIntegrity
	}
	if len(reader.File) == 0 || len(reader.File) > MaximumFiles+1 {
		return archiveContents{}, "", ErrTooLarge
	}
	entries := make(map[string][]byte, len(reader.File))
	var total int64
	for _, entry := range reader.File {
		if entry.FileInfo().IsDir() || path.Clean(entry.Name) != entry.Name || (entry.Name != manifestEntry && !fileEntryPattern.MatchString(entry.Name)) {
			return archiveContents{}, "", ErrIntegrity
		}
		if _, duplicate := entries[entry.Name]; duplicate {
			return archiveContents{}, "", ErrIntegrity
		}
		if entry.Method != zip.Store && entry.Method != zip.Deflate {
			return archiveContents{}, "", ErrIntegrity
		}
		maximum := MaximumFileBytes
		if entry.Name == manifestEntry {
			maximum = MaximumManifestBytes
		}
		if entry.UncompressedSize64 > uint64(maximum) || entry.UncompressedSize64 > entry.CompressedSize64*200+(1<<20) {
			return archiveContents{}, "", ErrTooLarge
		}
		total += int64(entry.UncompressedSize64)
		if total > MaximumUncompressedBytes {
			return archiveContents{}, "", ErrTooLarge
		}
		opened, openErr := entry.Open()
		if openErr != nil {
			return archiveContents{}, "", ErrIntegrity
		}
		value, readErr := readBounded(opened, maximum)
		closeErr := opened.Close()
		if readErr != nil || closeErr != nil || uint64(len(value)) != entry.UncompressedSize64 {
			return archiveContents{}, "", ErrIntegrity
		}
		entries[entry.Name] = value
	}
	manifestBytes, ok := entries[manifestEntry]
	if !ok {
		return archiveContents{}, "", ErrIntegrity
	}
	delete(entries, manifestEntry)
	var manifest Manifest
	if err := decodeStrictJSON(manifestBytes, &manifest); err != nil {
		return archiveContents{}, "", ErrIntegrity
	}
	if err := validateManifest(manifest, entries, true); err != nil {
		return archiveContents{}, "", err
	}
	return archiveContents{Manifest: manifest, Files: entries}, archiveChecksum, nil
}

func validateManifest(manifest Manifest, files map[string][]byte, verifyChecksums bool) error {
	if manifest.SchemaVersion != SchemaVersion || !stableIDPattern.MatchString(manifest.PackageID) ||
		!stableIDPattern.MatchString(manifest.SourceSystemID) || manifest.ExportedAt.IsZero() ||
		manifest.ExportedAt.Year() < 2000 || manifest.ExportedAt.Year() > 9999 ||
		(manifest.FileMode != FileModeMetadata && manifest.FileMode != FileModeInclude) ||
		len(manifest.Schemas) == 0 || len(manifest.Schemas) > MaximumRecords ||
		len(manifest.Records) == 0 || len(manifest.Records) > MaximumRecords || len(files) > MaximumFiles {
		return ErrInvalidInput
	}
	schemas := make(map[string]SchemaReference, len(manifest.Schemas))
	previousSchema := ""
	for _, schema := range manifest.Schemas {
		if !resourceTypePattern.MatchString(schema.RecordType) || !stableIDPattern.MatchString(schema.TemplateID) || schema.TemplateVersion < 1 || schema.RecordType <= previousSchema {
			return ErrInvalidInput
		}
		schemas[schema.RecordType] = schema
		previousSchema = schema.RecordType
	}
	seen := make(map[string]struct{}, len(manifest.Records))
	usedSchemas := make(map[string]struct{}, len(manifest.Schemas))
	referencedFiles := make(map[string]struct{})
	payloadTotal := 0
	fileCount := 0
	for _, record := range manifest.Records {
		if err := validateRecord(record); err != nil {
			return err
		}
		schema, ok := schemas[record.Type]
		if !ok || schema.TemplateID != record.TemplateID || schema.TemplateVersion != record.TemplateVersion {
			return ErrInvalidInput
		}
		usedSchemas[record.Type] = struct{}{}
		key := Reference{Type: record.Type, ID: record.ID}.Key()
		if _, duplicate := seen[key]; duplicate {
			return ErrInvalidInput
		}
		seen[key] = struct{}{}
		payloadTotal += len(record.Payload)
		if payloadTotal > MaximumPayloadTotalBytes {
			return ErrTooLarge
		}
		if verifyChecksums {
			checksum, err := checksumRecord(record)
			if err != nil || checksum != record.Checksum {
				return ErrIntegrity
			}
		}
		if record.File == nil {
			continue
		}
		fileCount++
		if fileCount > MaximumFiles {
			return ErrTooLarge
		}
		if record.File.Mode != manifest.FileMode {
			return ErrInvalidInput
		}
		if manifest.FileMode == FileModeMetadata {
			if record.File.Entry != "" {
				return ErrIntegrity
			}
			continue
		}
		if !fileEntryPattern.MatchString(record.File.Entry) || record.File.Entry != "files/"+record.File.SHA256 {
			return ErrIntegrity
		}
		content, ok := files[record.File.Entry]
		if !ok || int64(len(content)) != record.File.SizeBytes {
			return ErrIntegrity
		}
		fileDigest := sha256.Sum256(content)
		if hex.EncodeToString(fileDigest[:]) != record.File.SHA256 {
			return ErrIntegrity
		}
		referencedFiles[record.File.Entry] = struct{}{}
	}
	if manifest.FileMode == FileModeMetadata && len(files) != 0 {
		return ErrIntegrity
	}
	if len(referencedFiles) != len(files) {
		return ErrIntegrity
	}
	if len(usedSchemas) != len(schemas) {
		return ErrInvalidInput
	}
	return nil
}

func validateRecord(record Record) error {
	if !resourceTypePattern.MatchString(record.Type) || !stableIDPattern.MatchString(record.ID) || record.Revision < 1 ||
		!stableIDPattern.MatchString(record.TemplateID) || record.TemplateVersion < 1 {
		return fmt.Errorf("%w: invalid record identity or revision", ErrInvalidInput)
	}
	if len(record.Payload) == 0 || len(record.Payload) > MaximumPayloadBytes || !json.Valid(record.Payload) {
		return fmt.Errorf("%w: invalid record payload", ErrInvalidInput)
	}
	if len(record.Dependencies) > MaximumDependencies || record.Dependencies == nil {
		return fmt.Errorf("%w: invalid record dependency collection", ErrInvalidInput)
	}
	if !stableIDPattern.MatchString(record.Provenance.SourceSystemID) || !safeSourceRecordID(record.Provenance.SourceRecordID) {
		return fmt.Errorf("%w: invalid record provenance", ErrInvalidInput)
	}
	if record.Checksum != "" && !sha256Pattern.MatchString(record.Checksum) {
		return ErrInvalidInput
	}
	if record.Ownership.State != "local" && record.Ownership.State != "external_locked" && record.Ownership.State != "claimed" {
		return fmt.Errorf("%w: invalid record ownership state", ErrInvalidInput)
	}
	if err := validateOwnershipMetadata(record.Ownership); err != nil {
		return err
	}
	if err := validateSafeJSON(record.Payload); err != nil {
		return err
	}
	previous := ""
	for _, dependency := range record.Dependencies {
		if !resourceTypePattern.MatchString(dependency.Type) || !stableIDPattern.MatchString(dependency.ID) || dependency.Key() <= previous {
			return fmt.Errorf("%w: invalid, duplicate, or unsorted record dependency", ErrInvalidInput)
		}
		previous = dependency.Key()
	}
	if record.File != nil {
		if record.File.SizeBytes < 0 || record.File.SizeBytes > MaximumFileBytes || !sha256Pattern.MatchString(record.File.SHA256) ||
			!safeFileName(record.File.Name) || !safeMediaType(record.File.MediaType) {
			return fmt.Errorf("%w: invalid record file metadata", ErrInvalidInput)
		}
	}
	return nil
}

func validateOwnershipMetadata(value OwnershipMetadata) error {
	validSource := stableIDPattern.MatchString(value.SourceSystemID) && safeSourceRecordID(value.SourceRecordID)
	switch value.State {
	case "local":
		if value.SourceSystemID != "" || value.SourceRecordID != "" || value.ClaimedAt != nil {
			return fmt.Errorf("%w: local ownership must not contain external source metadata", ErrInvalidInput)
		}
	case "external_locked":
		if !validSource || value.ClaimedAt != nil {
			return fmt.Errorf("%w: locked ownership metadata is invalid", ErrInvalidInput)
		}
	case "claimed":
		if !validSource || value.ClaimedAt == nil || value.ClaimedAt.IsZero() || value.ClaimedAt.Year() < 2000 || value.ClaimedAt.Year() > 9999 {
			return fmt.Errorf("%w: claimed ownership metadata is invalid", ErrInvalidInput)
		}
	}
	return nil
}

func safeSourceRecordID(value string) bool {
	if value == "" || strings.TrimSpace(value) != value || !utf8.ValidString(value) || utf8.RuneCountInString(value) > 256 {
		return false
	}
	for _, character := range value {
		if character < 0x20 || character == 0x7f {
			return false
		}
	}
	return true
}

func checksumRecord(record Record) (string, error) {
	record.Checksum = ""
	encoded, err := json.Marshal(recordChecksumEnvelope{
		Type: record.Type, ID: record.ID, Revision: record.Revision, TemplateID: record.TemplateID, TemplateVersion: record.TemplateVersion, Dependencies: record.Dependencies,
		Provenance: record.Provenance, Ownership: record.Ownership, File: record.File, Payload: record.Payload,
	})
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:]), nil
}

func validateSafeJSON(payload []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return ErrInvalidInput
	}
	if _, ok := value.(map[string]any); !ok || unsafeJSONValue(value) {
		return ErrInvalidInput
	}
	return nil
}

func unsafeJSONValue(value any) bool {
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			if sensitiveKeyPattern.MatchString(strings.ReplaceAll(key, "_", "")) || unsafeJSONValue(child) {
				return true
			}
		}
	case []any:
		for _, child := range typed {
			if unsafeJSONValue(child) {
				return true
			}
		}
	case string:
		parsed, err := url.Parse(typed)
		if err == nil && (strings.EqualFold(parsed.Scheme, "http") || strings.EqualFold(parsed.Scheme, "https")) {
			if parsed.User != nil {
				return true
			}
			for key := range parsed.Query() {
				normalized := strings.ToLower(strings.ReplaceAll(strings.ReplaceAll(key, "-", ""), "_", ""))
				if sensitiveKeyPattern.MatchString(normalized) || normalized == "sig" || strings.Contains(normalized, "signature") {
					return true
				}
			}
		}
	}
	return false
}

func safeFileName(value string) bool {
	if value == "" || !utf8.ValidString(value) || utf8.RuneCountInString(value) > 255 || strings.ContainsAny(value, "/\\\x00\r\n") {
		return false
	}
	for _, character := range value {
		if character < 0x20 || character == 0x7f {
			return false
		}
	}
	return true
}

func safeMediaType(value string) bool {
	parsed, parameters, err := mime.ParseMediaType(value)
	return err == nil && parsed == value && len(parameters) == 0 && len(value) <= 127
}

func readBounded(reader io.Reader, maximum int64) ([]byte, error) {
	limited := io.LimitReader(reader, maximum+1)
	contents, err := io.ReadAll(limited)
	if err != nil {
		return nil, err
	}
	if int64(len(contents)) > maximum {
		return nil, ErrTooLarge
	}
	return contents, nil
}

func decodeStrictJSON(contents []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(contents))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("trailing JSON content")
	}
	return nil
}

func canonicalJSON(payload []byte) ([]byte, error) {
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return nil, errors.New("trailing JSON content")
	}
	return json.Marshal(value)
}

func canonicalJSONEqual(payload []byte, value any) bool {
	if len(payload) == 0 || containsJSONWhitespaceOutsideStrings(payload) {
		return false
	}
	canonicalPayload, err := canonicalJSON(payload)
	if err != nil {
		return false
	}
	encodedValue, err := json.Marshal(value)
	if err != nil {
		return false
	}
	canonicalValue, err := canonicalJSON(encodedValue)
	if err != nil {
		return false
	}
	return bytes.Equal(canonicalPayload, canonicalValue)
}

func containsJSONWhitespaceOutsideStrings(payload []byte) bool {
	inString := false
	escaped := false
	for _, character := range payload {
		switch {
		case inString:
			if escaped {
				escaped = false
				continue
			}
			if character == '\\' {
				escaped = true
				continue
			}
			if character == '"' {
				inString = false
			}
		default:
			if character == '"' {
				inString = true
				continue
			}
			if character == ' ' || character == '\n' || character == '\r' || character == '\t' {
				return true
			}
		}
	}
	return false
}

func archiveErrorCode(err error) string {
	switch {
	case errors.Is(err, ErrTooLarge):
		return "package_too_large"
	case errors.Is(err, ErrIntegrity):
		return "integrity_failed"
	case errors.Is(err, ErrDependencyMissing):
		return "dependency_missing"
	case errors.Is(err, ErrConflict):
		return "package_conflict"
	default:
		return "import_failed"
	}
}

func countManifestFiles(manifest Manifest) int {
	count := 0
	for _, record := range manifest.Records {
		if record.File != nil && record.File.Entry != "" {
			count++
		}
	}
	return count
}

func normalizedNow(value time.Time) time.Time { return value.UTC().Truncate(time.Microsecond) }
