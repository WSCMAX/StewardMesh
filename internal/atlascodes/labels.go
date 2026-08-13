package atlascodes

// Requirement: REQ-ATLAS-CODES-001. Features: inventory.identifiers, templates.schemas.

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"html"
	"math"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/maxlemke/stewardmesh/internal/domain"
	"github.com/maxlemke/stewardmesh/internal/patterns"
	qrcode "github.com/skip2/go-qrcode"
)

const (
	LabelTemplateRecordType   = "atlas.label-template"
	MaximumLabelBatch         = 50
	minimumLabelDimensionMM   = 12.0
	maximumLabelDimensionMM   = 300.0
	minimumQuietZoneMM        = 1.0
	minimumBarcodeModuleMM    = 0.19
	minimumQRModuleMM         = 0.25
	maximumLabelTextRunes     = 80
	maximumCachedBatches      = 128
	maximumCachedBatchBytes   = 8 << 20
	maximumRememberedRequests = 4096
	maximumLabelArtifactBytes = 4 << 20
)

var (
	ErrBatchCancelled         = errors.New("Atlas Codes label batch was cancelled")
	ErrIdempotencyConflict    = errors.New("Atlas Codes label idempotency key conflicts with another request")
	ErrIdempotencyExpired     = errors.New("Atlas Codes label idempotency replay is no longer retained")
	ErrUnsupportedLabelOutput = errors.New("unsupported Atlas Codes label output")
)

type LabelTemplate struct {
	ID                   string    `json:"id"`
	PatternTemplateID    string    `json:"patternTemplateId"`
	PatternVersion       int64     `json:"patternVersion"`
	Name                 string    `json:"name"`
	Description          string    `json:"description,omitempty"`
	Version              int64     `json:"version"`
	WidthMM              float64   `json:"widthMm"`
	HeightMM             float64   `json:"heightMm"`
	MarginMM             float64   `json:"marginMm"`
	QuietZoneMM          float64   `json:"quietZoneMm"`
	Symbology            Symbology `json:"symbology"`
	PayloadSource        string    `json:"payloadSource"`
	HumanReadableField   string    `json:"humanReadableField"`
	SafeAssetFields      []string  `json:"safeAssetFields,omitempty"`
	OrganizationBranding string    `json:"organizationBranding,omitempty"`
}

type LabelTemplateReader interface {
	ListTemplates(context.Context, patterns.ListQuery) ([]patterns.Template, error)
	GetTemplate(context.Context, string, int64) (patterns.Template, error)
}

type LabelOutput string

const (
	LabelOutputSVG LabelOutput = "svg"
	LabelOutputPDF LabelOutput = "pdf"
	LabelOutputZPL LabelOutput = "zpl"
)

type LabelBatchInput struct {
	IdempotencyKey  string      `json:"-"`
	TemplateID      string      `json:"templateId"`
	TemplateVersion int64       `json:"templateVersion"`
	IdentifierIDs   []string    `json:"identifierIds"`
	Output          LabelOutput `json:"output"`
	TestPrint       bool        `json:"testPrint,omitempty"`
}

type LabelBatch struct {
	ID        string        `json:"id"`
	Template  LabelTemplate `json:"template"`
	Output    LabelOutput   `json:"output"`
	TestPrint bool          `json:"testPrint"`
	ItemCount int           `json:"itemCount"`
	MediaType string        `json:"mediaType"`
	FileName  string        `json:"fileName"`
	Contents  []byte        `json:"-"`
	SHA256    string        `json:"sha256"`
	CreatedAt time.Time     `json:"createdAt"`
}

type LabelRenderer interface {
	Output() LabelOutput
	MediaType() string
	Extension() string
	Render(context.Context, LabelTemplate, []LabelRecord, bool) ([]byte, error)
}

// PrinterTransport deliberately consumes rendered bytes rather than domain
// associations. The browser transport returns a downloadable artifact and
// requires the operator to choose a printer in the OS dialog. Direct network
// transports can be added behind this boundary without entering label payloads.
type PrinterTransport interface {
	Deliver(context.Context, LabelBatch) error
}

type LabelRecord struct {
	IdentifierID   string
	AssetID        string
	EncodedPayload string
	HumanReadable  string
	AssetName      string
	AssetTag       string
	Branding       string
}

// LabelAssetAuthorizer runs after the organization-scoped identifier and asset
// have been resolved but before any cached or newly rendered bytes are
// returned. HTTP adapters use it to enforce current read, write, scope, and
// imported-ownership policy for every selected asset.
type LabelAssetAuthorizer func(context.Context, domain.Asset) error

type labelBatchCacheEntry struct {
	digest string
	batch  LabelBatch
}

type labelReplayEntry struct {
	digest    string
	template  LabelTemplate
	records   []LabelRecord
	createdAt time.Time
}

type LabelService struct {
	identifiers    *Service
	assets         AssetReader
	templates      LabelTemplateReader
	renderers      map[LabelOutput]LabelRenderer
	now            func() time.Time
	mu             sync.Mutex
	batches        map[string]labelBatchCacheEntry
	batchOrder     []string
	batchBytes     int
	requestDigests map[string]string
	requestOrder   []string
	replays        map[string]labelReplayEntry
	replayOrder    []string
}

func NewLabelService(identifiers *Service, assets AssetReader, templates LabelTemplateReader, renderers []LabelRenderer, now func() time.Time) (*LabelService, error) {
	if identifiers == nil || assets == nil || templates == nil {
		return nil, errors.New("Atlas Codes identifier, asset, and Patterns services are required")
	}
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	available := make(map[LabelOutput]LabelRenderer)
	for _, renderer := range renderers {
		if renderer == nil || renderer.Output() == "" || available[renderer.Output()] != nil {
			return nil, errors.New("Atlas Codes label renderers must have unique outputs")
		}
		available[renderer.Output()] = renderer
	}
	if available[LabelOutputSVG] == nil || available[LabelOutputPDF] == nil || available[LabelOutputZPL] == nil {
		return nil, errors.New("Atlas Codes SVG, PDF, and ZPL renderers are required")
	}
	return &LabelService{
		identifiers:    identifiers,
		assets:         assets,
		templates:      templates,
		renderers:      available,
		now:            now,
		batches:        make(map[string]labelBatchCacheEntry),
		requestDigests: make(map[string]string),
		replays:        make(map[string]labelReplayEntry),
	}, nil
}

func DefaultLabelRenderers() []LabelRenderer {
	return []LabelRenderer{SVGLabelRenderer{}, PDFLabelRenderer{}, ZPLLabelRenderer{DotsPerMillimeter: 8}}
}

func (s *LabelService) ListTemplates(ctx context.Context) ([]LabelTemplate, error) {
	patternTemplates, err := s.templates.ListTemplates(ctx, patterns.ListQuery{RecordType: LabelTemplateRecordType})
	if err != nil {
		return nil, err
	}
	result := make([]LabelTemplate, 0, len(patternTemplates))
	for _, patternTemplate := range patternTemplates {
		template, err := labelTemplateFromPattern(patternTemplate)
		if err != nil {
			// Patterns permits general schemas for this record type. Only complete,
			// immutable physical definitions are advertised as printable templates.
			continue
		}
		result = append(result, template)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result, nil
}

func (s *LabelService) CreateBatch(ctx context.Context, input LabelBatchInput) (LabelBatch, bool, error) {
	return s.CreateBatchAuthorized(ctx, input, nil)
}

func (s *LabelService) CreateBatchAuthorized(ctx context.Context, input LabelBatchInput, authorize LabelAssetAuthorizer) (LabelBatch, bool, error) {
	normalized, digest, err := normalizeLabelBatchInput(input)
	if err != nil {
		return LabelBatch{}, false, err
	}
	if err := ctx.Err(); err != nil {
		return LabelBatch{}, false, ErrBatchCancelled
	}
	template, err := s.getTemplate(ctx, normalized.TemplateID, normalized.TemplateVersion)
	if err != nil {
		return LabelBatch{}, false, err
	}
	renderer := s.renderers[normalized.Output]
	if renderer == nil {
		return LabelBatch{}, false, ErrUnsupportedLabelOutput
	}
	resolvedIdentifiers := make([]Identifier, 0, len(normalized.IdentifierIDs))
	resolvedAssets := make([]domain.Asset, 0, len(normalized.IdentifierIDs))
	for _, identifierID := range normalized.IdentifierIDs {
		if err := ctx.Err(); err != nil {
			return LabelBatch{}, false, ErrBatchCancelled
		}
		identifier, err := s.identifiers.store.GetIdentifierByID(ctx, s.identifiers.organizationID, identifierID)
		if err != nil {
			return LabelBatch{}, false, err
		}
		if identifier.OrganizationID != s.identifiers.organizationID || identifier.Status != StatusActive || identifier.Symbology != template.Symbology {
			return LabelBatch{}, false, ErrNotFound
		}
		asset, err := s.assets.GetAsset(ctx, identifier.AssetID)
		if err != nil || asset.OrganizationID != s.identifiers.organizationID {
			return LabelBatch{}, false, ErrReferenceMissing
		}
		if authorize != nil {
			if err := authorize(ctx, asset); err != nil {
				return LabelBatch{}, false, err
			}
		}
		resolvedIdentifiers = append(resolvedIdentifiers, identifier)
		resolvedAssets = append(resolvedAssets, asset)
	}
	s.mu.Lock()
	rememberedDigest, remembered := s.requestDigests[normalized.IdempotencyKey]
	if remembered && rememberedDigest != digest {
		s.mu.Unlock()
		return LabelBatch{}, false, ErrIdempotencyConflict
	}
	if existing, ok := s.batches[normalized.IdempotencyKey]; ok {
		s.mu.Unlock()
		if existing.digest != digest {
			return LabelBatch{}, false, ErrIdempotencyConflict
		}
		return cloneLabelBatch(existing.batch), false, nil
	}
	createdAt := time.Time{}
	var records []LabelRecord
	if remembered {
		replay, ok := s.replays[normalized.IdempotencyKey]
		if !ok || replay.digest != digest {
			s.mu.Unlock()
			return LabelBatch{}, false, ErrIdempotencyExpired
		}
		template = cloneLabelTemplate(replay.template)
		records = append([]LabelRecord(nil), replay.records...)
		createdAt = replay.createdAt
	}
	s.mu.Unlock()
	if !remembered {
		records = make([]LabelRecord, 0, len(resolvedIdentifiers))
		for index, identifier := range resolvedIdentifiers {
			record, err := labelRecord(template, identifier, resolvedAssets[index])
			if err != nil {
				return LabelBatch{}, false, err
			}
			records = append(records, record)
		}
	}
	if normalized.Output == LabelOutputSVG && len(records) != 1 {
		// An SVG is one physical browser page. Browsers cannot reliably paginate
		// inside a tall image without scaling or clipping it; selected batches use
		// the vector PDF or device adapter paths instead.
		return LabelBatch{}, false, ErrInvalidInput
	}
	contents, err := renderer.Render(ctx, template, records, normalized.TestPrint)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return LabelBatch{}, false, ErrBatchCancelled
		}
		return LabelBatch{}, false, err
	}
	if len(contents) == 0 || len(contents) > maximumLabelArtifactBytes {
		return LabelBatch{}, false, ErrInvalidInput
	}
	checksum := sha256.Sum256(contents)
	batchID := "label-batch-" + digest[:24]
	if createdAt.IsZero() {
		createdAt = s.now().UTC()
	}
	batch := LabelBatch{
		ID: batchID, Template: template, Output: normalized.Output, TestPrint: normalized.TestPrint,
		ItemCount: len(records), MediaType: renderer.MediaType(),
		FileName: batchID + "." + renderer.Extension(), Contents: append([]byte(nil), contents...),
		SHA256: hex.EncodeToString(checksum[:]), CreatedAt: createdAt,
	}
	s.mu.Lock()
	rememberedDigest, remembered = s.requestDigests[normalized.IdempotencyKey]
	if remembered && rememberedDigest != digest {
		s.mu.Unlock()
		return LabelBatch{}, false, ErrIdempotencyConflict
	}
	if existing, ok := s.batches[normalized.IdempotencyKey]; ok {
		s.mu.Unlock()
		if existing.digest != digest {
			return LabelBatch{}, false, ErrIdempotencyConflict
		}
		return cloneLabelBatch(existing.batch), false, nil
	}
	if !remembered {
		s.requestDigests[normalized.IdempotencyKey] = digest
		s.requestOrder = append(s.requestOrder, normalized.IdempotencyKey)
		for len(s.requestOrder) > maximumRememberedRequests {
			oldest := s.requestOrder[0]
			delete(s.requestDigests, oldest)
			s.requestOrder = append([]string(nil), s.requestOrder[1:]...)
		}
		s.replays[normalized.IdempotencyKey] = labelReplayEntry{
			digest: digest, template: cloneLabelTemplate(template), records: append([]LabelRecord(nil), records...), createdAt: batch.CreatedAt,
		}
		s.replayOrder = append(s.replayOrder, normalized.IdempotencyKey)
		for len(s.replayOrder) > maximumCachedBatches {
			oldest := s.replayOrder[0]
			delete(s.replays, oldest)
			s.replayOrder = append([]string(nil), s.replayOrder[1:]...)
		}
	}
	s.batches[normalized.IdempotencyKey] = labelBatchCacheEntry{digest: digest, batch: cloneLabelBatch(batch)}
	s.batchOrder = append(s.batchOrder, normalized.IdempotencyKey)
	s.batchBytes += len(batch.Contents)
	for len(s.batchOrder) > maximumCachedBatches || s.batchBytes > maximumCachedBatchBytes {
		oldest := s.batchOrder[0]
		s.batchBytes -= len(s.batches[oldest].batch.Contents)
		delete(s.batches, oldest)
		s.batchOrder = append([]string(nil), s.batchOrder[1:]...)
	}
	s.mu.Unlock()
	return batch, !remembered, nil
}

func (s *LabelService) getTemplate(ctx context.Context, id string, version int64) (LabelTemplate, error) {
	id = strings.TrimSpace(id)
	if !stableIDPattern.MatchString(id) {
		return LabelTemplate{}, ErrInvalidInput
	}
	patternTemplate, err := s.templates.GetTemplate(ctx, id, version)
	if err != nil {
		if errors.Is(err, patterns.ErrInvalidInput) {
			return LabelTemplate{}, ErrInvalidInput
		}
		if errors.Is(err, patterns.ErrNotFound) {
			return LabelTemplate{}, ErrNotFound
		}
		return LabelTemplate{}, err
	}
	if patternTemplate.RecordType != LabelTemplateRecordType || patternTemplate.Status != patterns.StatusActive {
		return LabelTemplate{}, ErrNotFound
	}
	return labelTemplateFromPattern(patternTemplate)
}

func labelTemplateFromPattern(patternTemplate patterns.Template) (LabelTemplate, error) {
	template := LabelTemplate{
		ID: patternTemplate.ID, PatternTemplateID: patternTemplate.ID, PatternVersion: patternTemplate.Version,
		Name: patternTemplate.Name, Description: patternTemplate.Description, Version: patternTemplate.Version,
	}
	for _, field := range patternTemplate.Fields {
		switch field.Key {
		case "symbology":
			if len(field.Options) == 1 {
				template.Symbology = Symbology(field.Options[0])
			}
		case "widthMm":
			template.WidthMM = fixedPatternNumber(field)
		case "heightMm":
			template.HeightMM = fixedPatternNumber(field)
		case "marginMm":
			template.MarginMM = fixedPatternNumber(field)
		case "quietZoneMm":
			template.QuietZoneMM = fixedPatternNumber(field)
		case "payloadSource":
			if len(field.Options) == 1 {
				template.PayloadSource = field.Options[0]
			}
		case "humanReadableField":
			if len(field.Options) == 1 {
				template.HumanReadableField = strings.TrimSpace(field.Options[0])
			}
		case "safeAssetFields":
			if len(field.Options) == 1 {
				for _, value := range strings.Split(field.Options[0], ",") {
					if value = strings.TrimSpace(value); value != "" {
						template.SafeAssetFields = append(template.SafeAssetFields, value)
					}
				}
			}
		case "organizationBranding":
			if len(field.Options) == 1 {
				template.OrganizationBranding = strings.TrimSpace(field.Options[0])
			}
		}
	}
	if err := validateLabelTemplate(template); err != nil {
		return LabelTemplate{}, err
	}
	return template, nil
}

func fixedPatternNumber(field patterns.Field) float64 {
	if field.Minimum == nil || field.Maximum == nil || *field.Minimum != *field.Maximum {
		return 0
	}
	return *field.Minimum
}

func normalizeLabelBatchInput(input LabelBatchInput) (LabelBatchInput, string, error) {
	input.IdempotencyKey = strings.TrimSpace(input.IdempotencyKey)
	input.TemplateID = strings.TrimSpace(input.TemplateID)
	if !stableIDPattern.MatchString(input.IdempotencyKey) || input.TemplateVersion < 1 || len(input.IdentifierIDs) < 1 || len(input.IdentifierIDs) > MaximumLabelBatch {
		return LabelBatchInput{}, "", ErrInvalidInput
	}
	if input.Output == "" {
		input.Output = LabelOutputSVG
	}
	if input.Output != LabelOutputSVG && input.Output != LabelOutputPDF && input.Output != LabelOutputZPL {
		return LabelBatchInput{}, "", ErrUnsupportedLabelOutput
	}
	seen := make(map[string]bool, len(input.IdentifierIDs))
	for index, id := range input.IdentifierIDs {
		id = strings.TrimSpace(id)
		if !stableIDPattern.MatchString(id) || seen[id] {
			return LabelBatchInput{}, "", ErrInvalidInput
		}
		seen[id], input.IdentifierIDs[index] = true, id
	}
	digestBytes := sha256.Sum256([]byte(strings.Join([]string{input.TemplateID, strconv.FormatInt(input.TemplateVersion, 10), string(input.Output), strconv.FormatBool(input.TestPrint), strings.Join(input.IdentifierIDs, "\x00")}, "\x1f")))
	return input, hex.EncodeToString(digestBytes[:]), nil
}

func validateLabelTemplate(template LabelTemplate) error {
	if template.Version < 1 || template.PatternVersion != template.Version || template.ID != template.PatternTemplateID || !stableIDPattern.MatchString(template.PatternTemplateID) ||
		template.WidthMM < minimumLabelDimensionMM || template.WidthMM > maximumLabelDimensionMM ||
		template.HeightMM < minimumLabelDimensionMM || template.HeightMM > maximumLabelDimensionMM ||
		template.MarginMM < 0 || template.QuietZoneMM < minimumQuietZoneMM ||
		2*(template.MarginMM+template.QuietZoneMM) >= math.Min(template.WidthMM, template.HeightMM) ||
		(template.Symbology != SymbologyCode128 && template.Symbology != SymbologyQR) ||
		(template.PayloadSource != "identifier_value" && template.PayloadSource != "organization_route") ||
		(template.Symbology == SymbologyQR && template.PayloadSource != "organization_route") ||
		(template.Symbology == SymbologyCode128 && template.PayloadSource != "identifier_value") ||
		template.HumanReadableField != "identifier.displayValue" || !validLabelText(template.OrganizationBranding) {
		return ErrInvalidInput
	}
	contentHeight := template.HeightMM - 2*template.MarginMM
	if template.Symbology == SymbologyCode128 && contentHeight < 19.5 {
		return ErrInvalidInput
	}
	if template.Symbology == SymbologyQR {
		codeSize := math.Min(contentHeight, template.WidthMM*0.44)
		textWidth := template.WidthMM - 2*template.MarginMM - codeSize - 2
		if contentHeight < 17 || textWidth < 10 {
			return ErrInvalidInput
		}
	}
	seen := make(map[string]bool, len(template.SafeAssetFields))
	for _, field := range template.SafeAssetFields {
		if (field != "asset.name" && field != "asset.assetTag") || seen[field] {
			return ErrInvalidInput
		}
		seen[field] = true
	}
	return nil
}

func labelRecord(template LabelTemplate, identifier Identifier, asset domain.Asset) (LabelRecord, error) {
	payload := identifier.NormalizedValue
	if template.PayloadSource == "organization_route" {
		var err error
		payload, err = LabelRoute(identifier.ID)
		if err != nil {
			return LabelRecord{}, err
		}
	}
	human := strings.TrimSpace(identifier.DisplayValue)
	if !validLabelText(human) || utf8.RuneCountInString(human) > maximumLabelTextRunes {
		return LabelRecord{}, ErrInvalidInput
	}
	record := LabelRecord{IdentifierID: identifier.ID, AssetID: asset.ID, EncodedPayload: payload, HumanReadable: human, Branding: template.OrganizationBranding}
	for _, field := range template.SafeAssetFields {
		switch field {
		case "asset.name":
			record.AssetName = strings.TrimSpace(asset.Name)
			if !validLabelText(record.AssetName) {
				return LabelRecord{}, ErrInvalidInput
			}
		case "asset.assetTag":
			record.AssetTag = strings.TrimSpace(asset.AssetTag)
			if !validLabelText(record.AssetTag) {
				return LabelRecord{}, ErrInvalidInput
			}
		}
	}
	if err := validateRecordFits(template, record); err != nil {
		return LabelRecord{}, err
	}
	return record, nil
}

func validateRecordFits(template LabelTemplate, record LabelRecord) error {
	textWidth := template.WidthMM - 2*template.MarginMM
	if template.Symbology == SymbologyCode128 {
		modules, err := code128Modules(record.EncodedPayload)
		if err != nil {
			return err
		}
		available := template.WidthMM - 2*(template.MarginMM+template.QuietZoneMM)
		module := math.Min(available/float64(len(modules)-20), template.QuietZoneMM/10)
		if module < minimumBarcodeModuleMM || template.QuietZoneMM < 10*minimumBarcodeModuleMM {
			return ErrInvalidInput
		}
	} else {
		matrix, err := qrMatrix(record.EncodedPayload)
		if err != nil {
			return err
		}
		size := math.Min(template.HeightMM-2*template.MarginMM, template.WidthMM*0.44)
		module := size / float64(len(matrix))
		if module < minimumQRModuleMM || 4*module < template.QuietZoneMM {
			return ErrInvalidInput
		}
		textWidth = template.WidthMM - template.MarginMM - size - 2 - template.MarginMM
	}
	for _, value := range []string{record.HumanReadable, record.AssetName, record.AssetTag, record.Branding} {
		if utf8.RuneCountInString(value) > int(math.Max(1, textWidth/1.65)) {
			return ErrInvalidInput
		}
	}
	if template.Symbology == SymbologyCode128 && utf8.RuneCountInString(assetSummary(record)) > int(math.Max(1, textWidth/1.65)) {
		return ErrInvalidInput
	}
	return nil
}

func validLabelText(value string) bool {
	if value == "" {
		return true
	}
	if !utf8.ValidString(value) || !printable(value) || utf8.RuneCountInString(value) > maximumLabelTextRunes {
		return false
	}
	for _, character := range value {
		if character < 0x20 || character > 0x7e {
			return false
		}
	}
	return true
}
func assetSummary(record LabelRecord) string {
	values := make([]string, 0, 2)
	if record.AssetName != "" {
		values = append(values, record.AssetName)
	}
	if record.AssetTag != "" {
		values = append(values, record.AssetTag)
	}
	return strings.Join(values, " / ")
}
func cloneLabelBatch(batch LabelBatch) LabelBatch {
	batch.Contents = append([]byte(nil), batch.Contents...)
	batch.Template = cloneLabelTemplate(batch.Template)
	return batch
}

func cloneLabelTemplate(template LabelTemplate) LabelTemplate {
	template.SafeAssetFields = append([]string(nil), template.SafeAssetFields...)
	return template
}

type SVGLabelRenderer struct{}

func (SVGLabelRenderer) Output() LabelOutput { return LabelOutputSVG }
func (SVGLabelRenderer) MediaType() string   { return "image/svg+xml" }
func (SVGLabelRenderer) Extension() string   { return "svg" }
func (SVGLabelRenderer) Render(ctx context.Context, template LabelTemplate, records []LabelRecord, testPrint bool) ([]byte, error) {
	if err := validateRenderInput(ctx, template, records); err != nil {
		return nil, err
	}
	if len(records) != 1 {
		return nil, ErrInvalidInput
	}
	var body strings.Builder
	height := template.HeightMM * float64(len(records))
	fmt.Fprintf(&body, `<svg xmlns="http://www.w3.org/2000/svg" width="%.2fmm" height="%.2fmm" viewBox="0 0 %.2f %.2f" role="img" aria-label="Atlas Codes label sheet">`, template.WidthMM, height, template.WidthMM, height)
	body.WriteString(`<rect width="100%" height="100%" fill="#fff"/>`)
	for index, record := range records {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		y := float64(index) * template.HeightMM
		if err := writeSVGLabel(&body, template, record, y, testPrint); err != nil {
			return nil, err
		}
	}
	body.WriteString(`</svg>`)
	return []byte(body.String()), nil
}

func writeSVGLabel(body *strings.Builder, template LabelTemplate, record LabelRecord, y float64, testPrint bool) error {
	dash := ""
	if testPrint {
		dash = ` stroke-dasharray="1 1"`
	}
	fmt.Fprintf(body, `<g transform="translate(0 %.2f)"><rect x="0.25" y="0.25" width="%.2f" height="%.2f" fill="#fff" stroke="#000" stroke-width="0.3"%s/>`, y, template.WidthMM-0.5, template.HeightMM-0.5, dash)
	if template.Symbology == SymbologyQR {
		matrix, err := qrMatrix(record.EncodedPayload)
		if err != nil {
			return err
		}
		size := math.Min(template.HeightMM-2*template.MarginMM, template.WidthMM*0.44)
		if size <= 0 {
			return ErrInvalidInput
		}
		module := size / float64(len(matrix))
		x, top := template.MarginMM, template.MarginMM
		for row := range matrix {
			for column, dark := range matrix[row] {
				if dark {
					fmt.Fprintf(body, `<rect x="%.4f" y="%.4f" width="%.4f" height="%.4f" fill="#000"/>`, x+float64(column)*module, top+float64(row)*module, module, module)
				}
			}
		}
		textX := x + size + 2
		writeSVGQRText(body, textX, template.MarginMM+3, template.WidthMM-textX-template.MarginMM, template.HeightMM, record)
	} else {
		modules, err := code128Modules(record.EncodedPayload)
		if err != nil {
			return err
		}
		x, top, availableWidth, barHeight := template.MarginMM+template.QuietZoneMM, template.MarginMM+3.2, template.WidthMM-2*(template.MarginMM+template.QuietZoneMM), template.HeightMM-2*template.MarginMM-12.5
		if barHeight < 7 {
			return ErrInvalidInput
		}
		symbol := modules[10 : len(modules)-10]
		module := math.Min(availableWidth/float64(len(symbol)), template.QuietZoneMM/10)
		for index, dark := range symbol {
			if dark {
				fmt.Fprintf(body, `<rect x="%.4f" y="%.4f" width="%.4f" height="%.4f" fill="#000"/>`, x+float64(index)*module, top, module, barHeight)
			}
		}
		writeSVGCode128Text(body, template, top+barHeight+3.5, record)
	}
	body.WriteString(`</g>`)
	return nil
}

func svgTextGroup(body *strings.Builder, x, available, height float64, record LabelRecord) string {
	digest := sha256.Sum256([]byte(record.IdentifierID))
	clipID := "clip-" + hex.EncodeToString(digest[:6])
	fmt.Fprintf(body, `<defs><clipPath id="%s"><rect x="%.2f" y="0" width="%.2f" height="%.2f"/></clipPath></defs><g clip-path="url(#%s)" fill="#000" font-family="sans-serif">`, clipID, x, math.Max(available, 1), height, clipID)
	return clipID
}

func writeSVGCode128Text(body *strings.Builder, template LabelTemplate, humanY float64, record LabelRecord) {
	svgTextGroup(body, template.MarginMM, template.WidthMM-2*template.MarginMM, template.HeightMM, record)
	if record.Branding != "" {
		fmt.Fprintf(body, `<text x="%.2f" y="%.2f" font-size="2.1">%s</text>`, template.MarginMM, template.MarginMM+2.2, html.EscapeString(record.Branding))
	}
	fmt.Fprintf(body, `<text x="%.2f" y="%.2f" font-size="3.2" font-weight="700">%s</text>`, template.MarginMM, humanY, html.EscapeString(record.HumanReadable))
	if summary := assetSummary(record); summary != "" {
		fmt.Fprintf(body, `<text x="%.2f" y="%.2f" font-size="2.4">%s</text>`, template.MarginMM, humanY+3.5, html.EscapeString(summary))
	}
	body.WriteString(`</g>`)
}

func writeSVGQRText(body *strings.Builder, x, y, available, height float64, record LabelRecord) {
	svgTextGroup(body, x, available, height, record)
	if record.Branding != "" {
		fmt.Fprintf(body, `<text x="%.2f" y="%.2f" font-size="2.1">%s</text>`, x, y, html.EscapeString(record.Branding))
	}
	fmt.Fprintf(body, `<text x="%.2f" y="%.2f" font-size="3.0" font-weight="700">%s</text>`, x, y+4.2, html.EscapeString(record.HumanReadable))
	if record.AssetName != "" {
		fmt.Fprintf(body, `<text x="%.2f" y="%.2f" font-size="2.5">%s</text>`, x, y+8.2, html.EscapeString(record.AssetName))
	}
	if record.AssetTag != "" {
		fmt.Fprintf(body, `<text x="%.2f" y="%.2f" font-size="2.3">%s</text>`, x, y+11.8, html.EscapeString(record.AssetTag))
	}
	body.WriteString(`</g>`)
}

func validateRenderInput(ctx context.Context, template LabelTemplate, records []LabelRecord) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := validateLabelTemplate(template); err != nil {
		return err
	}
	if len(records) < 1 || len(records) > MaximumLabelBatch {
		return ErrInvalidInput
	}
	for _, record := range records {
		if _, normalized, err := normalizeCode(template.Symbology, record.EncodedPayload); err != nil || normalized != record.EncodedPayload {
			return ErrInvalidInput
		}
		if template.PayloadSource == "organization_route" {
			identifierID := strings.TrimPrefix(record.EncodedPayload, labelRoutePrefix)
			if !strings.HasPrefix(record.EncodedPayload, labelRoutePrefix) || !stableIDPattern.MatchString(identifierID) || strings.Contains(identifierID, "/") {
				return ErrInvalidInput
			}
		}
		if !validLabelText(record.HumanReadable) || !validLabelText(record.AssetName) || !validLabelText(record.AssetTag) || !validLabelText(record.Branding) {
			return ErrInvalidInput
		}
		if err := validateRecordFits(template, record); err != nil {
			return err
		}
	}
	return nil
}

func qrMatrix(payload string) ([][]bool, error) {
	qr, err := qrcode.New(payload, qrcode.Medium)
	if err != nil {
		return nil, fmt.Errorf("render QR payload: %w", err)
	}
	return qr.Bitmap(), nil
}

// Code 128-B symbols. The stop pattern has seven elements; every other symbol
// has six. One leading and trailing 10-module quiet zone is added explicitly.
var code128Widths = [...]string{
	"212222", "222122", "222221", "121223", "121322", "131222", "122213", "122312", "132212", "221213", "221312", "231212", "112232", "122132", "122231", "113222", "123122", "123221", "223211", "221132", "221231", "213212", "223112", "312131", "311222", "321122", "321221", "312212", "322112", "322211", "212123", "212321", "232121", "111323", "131123", "131321", "112313", "132113", "132311", "211313", "231113", "231311", "112133", "112331", "132131", "113123", "113321", "133121", "313121", "211331", "231131", "213113", "213311", "213131", "311123", "311321", "331121", "312113", "312311", "332111", "314111", "221411", "431111", "111224", "111422", "121124", "121421", "141122", "141221", "112214", "112412", "122114", "122411", "142112", "142211", "241211", "221114", "413111", "241112", "134111", "111242", "121142", "121241", "114212", "124112", "124211", "411212", "421112", "421211", "212141", "214121", "412121", "111143", "111341", "131141", "114113", "114311", "411113", "411311", "113141", "114131", "311141", "411131", "211412", "211214", "211232", "2331112",
}

func code128Modules(payload string) ([]bool, error) {
	if len(payload) < 1 || len(payload) > maximumCode128Bytes {
		return nil, ErrInvalidInput
	}
	codes := make([]int, 0, len(payload)+3)
	codes = append(codes, 104)
	checksum := 104
	for index := range len(payload) {
		if payload[index] < 32 || payload[index] > 126 {
			return nil, ErrInvalidInput
		}
		code := int(payload[index]) - 32
		codes = append(codes, code)
		checksum += code * (index + 1)
	}
	codes = append(codes, checksum%103, 106)
	modules := make([]bool, 10, 11*len(codes)+21)
	for _, code := range codes {
		dark := true
		for _, digit := range code128Widths[code] {
			width := int(digit - '0')
			for range width {
				modules = append(modules, dark)
			}
			dark = !dark
		}
	}
	modules = append(modules, make([]bool, 10)...)
	return modules, nil
}

type ZPLLabelRenderer struct{ DotsPerMillimeter int }

func (ZPLLabelRenderer) Output() LabelOutput { return LabelOutputZPL }
func (ZPLLabelRenderer) MediaType() string   { return "application/vnd.zebra-zpl" }
func (ZPLLabelRenderer) Extension() string   { return "zpl" }
func (renderer ZPLLabelRenderer) Render(ctx context.Context, template LabelTemplate, records []LabelRecord, testPrint bool) ([]byte, error) {
	if err := validateRenderInput(ctx, template, records); err != nil {
		return nil, err
	}
	dpm := renderer.DotsPerMillimeter
	if dpm != 8 && dpm != 12 {
		return nil, ErrInvalidInput
	}
	width, height := int(math.Round(template.WidthMM*float64(dpm))), int(math.Round(template.HeightMM*float64(dpm)))
	margin, quiet := int(math.Round(template.MarginMM*float64(dpm))), int(math.Round(template.QuietZoneMM*float64(dpm)))
	smallFont, normalFont, humanFont := int(math.Round(2.1*float64(dpm))), int(math.Round(2.4*float64(dpm))), int(math.Round(3.2*float64(dpm)))
	var output bytes.Buffer
	for _, record := range records {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		fmt.Fprintf(&output, "^XA\n^CI28\n^PW%d\n^LL%d\n^LH0,0\n", width, height)
		if testPrint {
			fmt.Fprintf(&output, "^FO%d,%d^GB%d,%d,2,B,0^FS\n", 2, 2, width-4, height-4)
		}
		if template.Symbology == SymbologyQR {
			matrix, err := qrMatrix(record.EncodedPayload)
			if err != nil {
				return nil, err
			}
			available := int(math.Floor(math.Min(template.HeightMM-2*template.MarginMM, template.WidthMM*0.44) * float64(dpm)))
			magnification := min(10, available/len(matrix))
			minimumMagnification := max(int(math.Ceil(minimumQRModuleMM*float64(dpm))), int(math.Ceil(float64(quiet)/4)))
			if magnification < minimumMagnification {
				return nil, ErrInvalidInput
			}
			codeSize := len(matrix) * magnification
			if margin+codeSize > height-margin || margin+codeSize > width-margin {
				return nil, ErrInvalidInput
			}
			fmt.Fprintf(&output, "^FO%d,%d^BQN,2,%d^FH^FDLA,%s^FS\n", margin, margin, magnification, zplData(record.EncodedPayload))
			textX, textWidth := margin+codeSize+2*dpm, width-(margin+codeSize+2*dpm)-margin
			if textWidth < 10*dpm {
				return nil, ErrInvalidInput
			}
			if record.Branding != "" {
				fmt.Fprintf(&output, "^FO%d,%d^A0N,%d,%d^FB%d,1,0,L,0^FH^FD%s^FS\n", textX, margin, smallFont, smallFont, textWidth, zplData(record.Branding))
			}
			fmt.Fprintf(&output, "^FO%d,%d^A0N,%d,%d^FB%d,1,0,L,0^FH^FD%s^FS\n", textX, margin+4*dpm, humanFont, humanFont, textWidth, zplData(record.HumanReadable))
			if record.AssetName != "" {
				fmt.Fprintf(&output, "^FO%d,%d^A0N,%d,%d^FB%d,1,0,L,0^FH^FD%s^FS\n", textX, margin+9*dpm, normalFont, normalFont, textWidth, zplData(record.AssetName))
			}
			if record.AssetTag != "" {
				fmt.Fprintf(&output, "^FO%d,%d^A0N,%d,%d^FB%d,1,0,L,0^FH^FD%s^FS\n", textX, margin+13*dpm, normalFont, normalFont, textWidth, zplData(record.AssetTag))
			}
		} else {
			modules, err := code128Modules(record.EncodedPayload)
			if err != nil {
				return nil, err
			}
			symbolModules := len(modules) - 20
			moduleDots := min((width-2*(margin+quiet))/symbolModules, quiet/10)
			if moduleDots < int(math.Ceil(minimumBarcodeModuleMM*float64(dpm))) {
				return nil, ErrInvalidInput
			}
			barTop := margin + int(math.Round(3.2*float64(dpm)))
			barHeight := int(math.Round((template.HeightMM - 2*template.MarginMM - 12.5) * float64(dpm)))
			if barHeight < 7*dpm || margin+quiet+symbolModules*moduleDots > width-margin-quiet {
				return nil, ErrInvalidInput
			}
			if record.Branding != "" {
				fmt.Fprintf(&output, "^FO%d,%d^A0N,%d,%d^FB%d,1,0,L,0^FH^FD%s^FS\n", margin, margin, smallFont, smallFont, width-2*margin, zplData(record.Branding))
			}
			fmt.Fprintf(&output, "^FO%d,%d^BY%d,3,%d^BCN,%d,N,N,N^FH^FD%s^FS\n", margin+quiet, barTop, moduleDots, barHeight, barHeight, zplData(record.EncodedPayload))
			humanTop := barTop + barHeight + dpm
			fmt.Fprintf(&output, "^FO%d,%d^A0N,%d,%d^FB%d,1,0,L,0^FH^FD%s^FS\n", margin, humanTop, humanFont, humanFont, width-2*margin, zplData(record.HumanReadable))
			if summary := assetSummary(record); summary != "" {
				fmt.Fprintf(&output, "^FO%d,%d^A0N,%d,%d^FB%d,1,0,L,0^FH^FD%s^FS\n", margin, humanTop+4*dpm, normalFont, normalFont, width-2*margin, zplData(summary))
			}
		}
		output.WriteString("^XZ\n")
	}
	return output.Bytes(), nil
}

func zplData(value string) string {
	var output strings.Builder
	for index := 0; index < len(value); index++ {
		character := value[index]
		if character == '^' || character == '~' || character == '_' || character < 32 || character > 126 {
			fmt.Fprintf(&output, "_%02X", character)
		} else {
			output.WriteByte(character)
		}
	}
	return output.String()
}

type PDFLabelRenderer struct{}

func (PDFLabelRenderer) Output() LabelOutput { return LabelOutputPDF }
func (PDFLabelRenderer) MediaType() string   { return "application/pdf" }
func (PDFLabelRenderer) Extension() string   { return "pdf" }
func (PDFLabelRenderer) Render(ctx context.Context, template LabelTemplate, records []LabelRecord, testPrint bool) ([]byte, error) {
	if err := validateRenderInput(ctx, template, records); err != nil {
		return nil, err
	}
	pageWidth, pageHeight := mmToPoints(template.WidthMM), mmToPoints(template.HeightMM)
	objects := []string{"", "<< /Type /Catalog /Pages 2 0 R >>", fmt.Sprintf("<< /Type /Pages /Kids [%s] /Count %d >>", pdfPageReferences(len(records)), len(records)), "<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>"}
	for index, record := range records {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		stream, err := pdfLabelStream(template, record, testPrint)
		if err != nil {
			return nil, err
		}
		contentNumber := 5 + index*2
		objects = append(objects, fmt.Sprintf("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 %.3f %.3f] /Resources << /Font << /F1 3 0 R >> >> /Contents %d 0 R >>", pageWidth, pageHeight, contentNumber))
		objects = append(objects, fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(stream), stream))
	}
	var pdf bytes.Buffer
	pdf.WriteString("%PDF-1.4\n%\xE2\xE3\xCF\xD3\n")
	offsets := make([]int, len(objects))
	for index := 1; index < len(objects); index++ {
		offsets[index] = pdf.Len()
		fmt.Fprintf(&pdf, "%d 0 obj\n%s\nendobj\n", index, objects[index])
	}
	xref := pdf.Len()
	fmt.Fprintf(&pdf, "xref\n0 %d\n0000000000 65535 f \n", len(objects))
	for index := 1; index < len(objects); index++ {
		fmt.Fprintf(&pdf, "%010d 00000 n \n", offsets[index])
	}
	fmt.Fprintf(&pdf, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objects), xref)
	return pdf.Bytes(), nil
}

func pdfPageReferences(count int) string {
	refs := make([]string, count)
	for index := range count {
		refs[index] = fmt.Sprintf("%d 0 R", 4+index*2)
	}
	return strings.Join(refs, " ")
}
func mmToPoints(value float64) float64 { return value * 72 / 25.4 }
func pdfLabelStream(template LabelTemplate, record LabelRecord, testPrint bool) (string, error) {
	width, height, margin, quiet := mmToPoints(template.WidthMM), mmToPoints(template.HeightMM), mmToPoints(template.MarginMM), mmToPoints(template.QuietZoneMM)
	var stream strings.Builder
	fmt.Fprintf(&stream, "1 1 1 rg 0 0 %.3f %.3f re f\n0 0 0 RG 0.7 w\n", width, height)
	if testPrint {
		stream.WriteString("[2 2] 0 d\n")
	}
	fmt.Fprintf(&stream, "0.5 0.5 %.3f %.3f re S\n", width-1, height-1)
	if testPrint {
		stream.WriteString("[] 0 d\n")
	}
	if template.Symbology == SymbologyQR {
		matrix, err := qrMatrix(record.EncodedPayload)
		if err != nil {
			return "", err
		}
		sizeMM := math.Min(template.HeightMM-2*template.MarginMM, template.WidthMM*0.44)
		size := mmToPoints(sizeMM)
		module := size / float64(len(matrix))
		x, top := margin, height-margin
		stream.WriteString("0 0 0 rg\n")
		for row := range matrix {
			for column, dark := range matrix[row] {
				if dark {
					fmt.Fprintf(&stream, "%.3f %.3f %.3f %.3f re f\n", x+float64(column)*module, top-size+float64(len(matrix)-1-row)*module, module+0.01, module+0.01)
				}
			}
		}
		textX := x + size + mmToPoints(2)
		pdfText(&stream, textX, height-mmToPoints(template.MarginMM+3), 6, record.Branding)
		pdfText(&stream, textX, height-mmToPoints(template.MarginMM+7.2), 9, record.HumanReadable)
		pdfText(&stream, textX, height-mmToPoints(template.MarginMM+11.2), 7, record.AssetName)
		pdfText(&stream, textX, height-mmToPoints(template.MarginMM+14.8), 6.5, record.AssetTag)
	} else {
		modules, err := code128Modules(record.EncodedPayload)
		if err != nil {
			return "", err
		}
		symbol := modules[10 : len(modules)-10]
		x := margin + quiet
		availableWidth := width - 2*(margin+quiet)
		barWidth := math.Min(availableWidth/float64(len(symbol)), quiet/10)
		barTopMM := template.MarginMM + 3.2
		barHeightMM := template.HeightMM - 2*template.MarginMM - 12.5
		barHeight := mmToPoints(barHeightMM)
		barBottom := height - mmToPoints(barTopMM+barHeightMM)
		stream.WriteString("0 0 0 rg\n")
		for index, dark := range symbol {
			if dark {
				fmt.Fprintf(&stream, "%.3f %.3f %.3f %.3f re f\n", x+float64(index)*barWidth, barBottom, barWidth+0.01, barHeight)
			}
		}
		pdfText(&stream, margin, height-mmToPoints(template.MarginMM+2.2), 6, record.Branding)
		humanYMM := barTopMM + barHeightMM + 3.5
		pdfText(&stream, margin, height-mmToPoints(humanYMM), 9, record.HumanReadable)
		pdfText(&stream, margin, height-mmToPoints(humanYMM+3.5), 7, assetSummary(record))
	}
	return stream.String(), nil
}

func pdfText(stream *strings.Builder, x, y, size float64, value string) {
	if value == "" {
		return
	}
	fmt.Fprintf(stream, "BT /F1 %.2f Tf %.3f %.3f Td (%s) Tj ET\n", size, x, y, pdfEscape(value))
}
func pdfEscape(value string) string {
	var out strings.Builder
	for _, character := range value {
		switch character {
		case '\\', '(', ')':
			out.WriteByte('\\')
			out.WriteRune(character)
		default:
			if character >= 32 && character <= 126 {
				out.WriteRune(character)
			} else {
				out.WriteByte('?')
			}
		}
	}
	return out.String()
}
