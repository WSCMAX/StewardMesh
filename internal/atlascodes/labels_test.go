package atlascodes

// Requirement: REQ-ATLAS-CODES-001. Features: inventory.identifiers, templates.schemas.

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/maxlemke/stewardmesh/internal/atlas"
	"github.com/maxlemke/stewardmesh/internal/domain"
	"github.com/maxlemke/stewardmesh/internal/foundation"
	"github.com/maxlemke/stewardmesh/internal/patterns"
)

type labelTestAssets struct{ items map[string]domain.Asset }

func (a labelTestAssets) GetAsset(_ context.Context, id string) (domain.Asset, error) {
	asset, ok := a.items[id]
	if !ok {
		return domain.Asset{}, atlas.ErrNotFound
	}
	return asset, nil
}

type labelPatternReader struct{ templates []patterns.Template }

type fixedSizeLabelRenderer struct{ size int }

func (fixedSizeLabelRenderer) Output() LabelOutput { return LabelOutputSVG }
func (fixedSizeLabelRenderer) MediaType() string   { return "image/svg+xml" }
func (fixedSizeLabelRenderer) Extension() string   { return "svg" }
func (renderer fixedSizeLabelRenderer) Render(ctx context.Context, _ LabelTemplate, records []LabelRecord, _ bool) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	contents := bytes.Repeat([]byte{'x'}, renderer.size)
	if len(records) > 0 {
		copy(contents, records[0].AssetName)
	}
	return contents, nil
}

func (r labelPatternReader) ListTemplates(_ context.Context, query patterns.ListQuery) ([]patterns.Template, error) {
	items := make([]patterns.Template, 0, len(r.templates))
	for _, template := range r.templates {
		if query.RecordType == "" || query.RecordType == template.RecordType {
			items = append(items, template)
		}
	}
	return items, nil
}

func (r labelPatternReader) GetTemplate(_ context.Context, id string, version int64) (patterns.Template, error) {
	for _, template := range r.templates {
		if id == template.ID && (version == 0 || version == template.Version) {
			return template, nil
		}
	}
	return patterns.Template{}, patterns.ErrNotFound
}

func labelPatternFixtures() []patterns.Template {
	items := make([]patterns.Template, 0, 2)
	for _, template := range patterns.BuiltInTemplates() {
		if template.RecordType == LabelTemplateRecordType {
			items = append(items, template)
		}
	}
	return items
}

func labelPatternFixture(t *testing.T, symbology Symbology) patterns.Template {
	t.Helper()
	for _, template := range labelPatternFixtures() {
		resolved, err := labelTemplateFromPattern(template)
		if err == nil && resolved.Symbology == symbology {
			return template
		}
	}
	t.Fatalf("missing %s label Pattern fixture", symbology)
	return patterns.Template{}
}

func newLabelTestService(t *testing.T) (*LabelService, *Service) {
	t.Helper()
	assets := labelTestAssets{items: map[string]domain.Asset{
		"asset-one": {ID: "asset-one", OrganizationID: "org-one", Name: "Lab server", AssetTag: "TAG-001", SerialNumber: "PRIVATE-SERIAL", DeploymentNotes: "PRIVATE-NOTES"},
		"asset-two": {ID: "asset-two", OrganizationID: "org-one", Name: "Field printer", AssetTag: "TAG-002"},
	}}
	identifiers, err := NewService(newIdentifierTestStore(), assets, foundation.NopAuditor{}, ServiceConfig{OrganizationID: "org-one", Now: func() time.Time {
		return time.Date(2026, time.August, 13, 12, 0, 0, 0, time.UTC)
	}})
	if err != nil {
		t.Fatal(err)
	}
	ctx := foundation.WithScope(context.Background(), foundation.Scope{OrganizationID: "org-one", ActorID: "operator-one", CorrelationID: "label-test-correlation"})
	for _, input := range []CreateIdentifierInput{
		{ID: "identifier-code", AssetID: "asset-one", Symbology: SymbologyCode128, Value: "LAB-001", DisplayValue: "LAB-001", Source: SourceGenerated},
		{ID: "identifier-code-two", AssetID: "asset-two", Symbology: SymbologyCode128, Value: "LAB-002", DisplayValue: "LAB-002", Source: SourceGenerated},
		{ID: "identifier-qr", AssetID: "asset-one", Symbology: SymbologyQR, Value: "CONFIDENTIAL-ASSET-DETAIL", DisplayValue: "Visible QR", Source: SourceGenerated},
	} {
		if _, created, err := identifiers.CreateIdentifier(ctx, input); err != nil || !created {
			t.Fatalf("create label fixture identifier: created=%t err=%v", created, err)
		}
	}
	labels, err := NewLabelService(identifiers, assets, labelPatternReader{templates: labelPatternFixtures()}, DefaultLabelRenderers(), func() time.Time {
		return time.Date(2026, time.August, 13, 12, 30, 0, 0, time.UTC)
	})
	if err != nil {
		t.Fatal(err)
	}
	return labels, identifiers
}

func TestLabelTemplatesReferenceImmutablePatternsVersionsAndPhysicalGeometry(t *testing.T) {
	labels, _ := newLabelTestService(t)
	templates, err := labels.ListTemplates(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(templates) != 2 {
		t.Fatalf("expected Code 128 and QR templates, got %#v", templates)
	}
	for _, template := range templates {
		if template.PatternTemplateID != template.ID || template.PatternVersion != 1 || template.Version != 1 ||
			template.WidthMM <= 0 || template.HeightMM <= 0 || template.MarginMM < 0 || template.QuietZoneMM < minimumQuietZoneMM ||
			template.HumanReadableField != "identifier.displayValue" || len(template.SafeAssetFields) != 2 {
			t.Fatalf("incomplete versioned label template: %#v", template)
		}
	}
}

func TestCustomPatternVersionDrivesLabelGeometryWithoutParsingItsID(t *testing.T) {
	labels, _ := newLabelTestService(t)
	custom := labelPatternFixture(t, SymbologyCode128)
	custom.ID = "custom--warehouse-label"
	custom.Name = "Warehouse label"
	custom.Version = 7
	custom.BuiltIn = false
	for index := range custom.Fields {
		if custom.Fields[index].Key == "widthMm" {
			width := 75.0
			custom.Fields[index].Minimum = &width
			custom.Fields[index].Maximum = &width
		}
	}
	labels.templates = labelPatternReader{templates: []patterns.Template{custom}}

	templates, err := labels.ListTemplates(context.Background())
	if err != nil || len(templates) != 1 {
		t.Fatalf("list custom label Pattern: %#v err=%v", templates, err)
	}
	if templates[0].ID != custom.ID || templates[0].PatternVersion != 7 || templates[0].WidthMM != 75 {
		t.Fatalf("label geometry did not come from the selected immutable Pattern version: %#v", templates[0])
	}
	batch, created, err := labels.CreateBatch(context.Background(), LabelBatchInput{
		IdempotencyKey: "custom-pattern-label", TemplateID: custom.ID, TemplateVersion: custom.Version,
		IdentifierIDs: []string{"identifier-code"}, Output: LabelOutputSVG,
	})
	if err != nil || !created || batch.Template.ID != custom.ID || !bytes.Contains(batch.Contents, []byte(`width="75.00mm"`)) {
		t.Fatalf("render custom Pattern geometry: %#v created=%t err=%v", batch.Template, created, err)
	}
}

func TestLabelBatchesAreBoundedCancellableAndIdempotentWithoutAssociationWrites(t *testing.T) {
	labels, identifiers := newLabelTestService(t)
	input := LabelBatchInput{IdempotencyKey: "preview-one", TemplateID: "builtin-atlas-label-code128", TemplateVersion: 1, IdentifierIDs: []string{"identifier-code"}, Output: LabelOutputSVG, TestPrint: true}
	batch, created, err := labels.CreateBatch(context.Background(), input)
	if err != nil || !created || batch.ItemCount != 1 || batch.MediaType != "image/svg+xml" || !bytes.HasPrefix(batch.Contents, []byte("<svg")) {
		t.Fatalf("unexpected SVG label batch %#v created=%t err=%v", batch, created, err)
	}
	retry, created, err := labels.CreateBatch(context.Background(), input)
	if err != nil || created || retry.ID != batch.ID || !bytes.Equal(retry.Contents, batch.Contents) {
		t.Fatalf("expected exact idempotent replay %#v created=%t err=%v", retry, created, err)
	}
	conflict := input
	conflict.Output = LabelOutputPDF
	if _, _, err := labels.CreateBatch(context.Background(), conflict); !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("expected idempotency conflict, got %v", err)
	}
	duplicate := input
	duplicate.IdempotencyKey = "duplicate"
	duplicate.IdentifierIDs = []string{"identifier-code", "identifier-code"}
	if _, _, err := labels.CreateBatch(context.Background(), duplicate); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("expected duplicate selection rejection, got %v", err)
	}
	oversized := input
	oversized.IdempotencyKey = "oversized"
	oversized.IdentifierIDs = make([]string, MaximumLabelBatch+1)
	for index := range oversized.IdentifierIDs {
		oversized.IdentifierIDs[index] = "identifier-" + strings.Repeat("x", index+1)
	}
	if _, _, err := labels.CreateBatch(context.Background(), oversized); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("expected batch bound rejection, got %v", err)
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	input.IdempotencyKey = "cancelled"
	if _, _, err := labels.CreateBatch(cancelled, input); !errors.Is(err, ErrBatchCancelled) {
		t.Fatalf("expected cancellation, got %v", err)
	}
	history, err := identifiers.ListIdentifiers(context.Background(), "asset-one")
	if err != nil || len(history) != 2 {
		t.Fatalf("label generation must not create duplicate associations: %#v err=%v", history, err)
	}
}

func TestReplacedCode128IdentifierCanGenerateSVGTestPrint(t *testing.T) {
	labels, identifiers := newLabelTestService(t)
	assets := labels.assets.(labelTestAssets)
	asset := assets.items["asset-one"]
	asset.Name = "Phase One Workstation 001"
	asset.AssetTag = "P1-ASSET-001"
	assets.items[asset.ID] = asset

	ctx := foundation.WithScope(context.Background(), foundation.Scope{
		OrganizationID: "org-one",
		ActorID:        "operator-one",
		CorrelationID:  "replacement-label-correlation",
	})
	replacement, changed, err := identifiers.ReplaceIdentifier(ctx, ReplaceIdentifierInput{
		AssetID:              asset.ID,
		IdentifierID:         "identifier-code",
		ReplacementID:        "identifier-code-replacement",
		ReplacementSymbology: SymbologyCode128,
		ReplacementValue:     "P1-CODE128-001-R2",
		DisplayValue:         "P1 Asset 001 replacement",
		Source:               SourceUserEntered,
		Revision:             1,
	})
	if err != nil || !changed || replacement.Status != StatusActive || replacement.SupersedesID != "identifier-code" {
		t.Fatalf("replace Code 128 identifier: replacement=%#v changed=%t err=%v", replacement, changed, err)
	}

	batch, created, err := labels.CreateBatch(ctx, LabelBatchInput{
		IdempotencyKey: "replacement-svg-test-print", TemplateID: "builtin-atlas-label-code128", TemplateVersion: 1,
		IdentifierIDs: []string{replacement.ID}, Output: LabelOutputSVG, TestPrint: true,
	})
	if err != nil || !created || batch.ItemCount != 1 || !batch.TestPrint ||
		!bytes.Contains(batch.Contents, []byte("P1 Asset 001 replacement")) ||
		!bytes.Contains(batch.Contents, []byte("Phase One Workstation 001 / P1-ASSET-001")) {
		t.Fatalf("render replacement SVG test print: created=%t batch=%#v err=%v", created, batch, err)
	}
}

func TestVectorOutputsUseSafeQRPointersAndRemainPrintable(t *testing.T) {
	labels, identifiers := newLabelTestService(t)
	for _, output := range []LabelOutput{LabelOutputSVG, LabelOutputPDF, LabelOutputZPL} {
		batch, created, err := labels.CreateBatch(context.Background(), LabelBatchInput{IdempotencyKey: "qr-" + string(output), TemplateID: "builtin-atlas-label-qr", TemplateVersion: 1, IdentifierIDs: []string{"identifier-qr"}, Output: output})
		if err != nil || !created || len(batch.Contents) == 0 {
			t.Fatalf("render %s: created=%t err=%v", output, created, err)
		}
		if bytes.Contains(batch.Contents, []byte("CONFIDENTIAL-ASSET-DETAIL")) || bytes.Contains(batch.Contents, []byte("PRIVATE-SERIAL")) || bytes.Contains(batch.Contents, []byte("PRIVATE-NOTES")) {
			t.Fatalf("%s output leaked a confidential value: %s", output, batch.Contents)
		}
		switch output {
		case LabelOutputSVG:
			if !bytes.Contains(batch.Contents, []byte(`width="50.00mm"`)) || !bytes.Contains(batch.Contents, []byte("<rect")) {
				t.Fatalf("SVG lacks physical dimensions or vector modules: %s", batch.Contents)
			}
		case LabelOutputPDF:
			if !bytes.HasPrefix(batch.Contents, []byte("%PDF-1.4")) || !bytes.Contains(batch.Contents, []byte("/MediaBox")) || !bytes.HasSuffix(batch.Contents, []byte("%%EOF\n")) {
				t.Fatalf("invalid vector PDF framing: %q", batch.Contents)
			}
		case LabelOutputZPL:
			if !bytes.Contains(batch.Contents, []byte("^FDLA,/atlas/codes/identifier-qr^FS")) {
				t.Fatalf("ZPL does not contain the safe application route: %s", batch.Contents)
			}
		}
	}
	resolved, err := identifiers.ResolveIdentifier(context.Background(), SymbologyQR, "/atlas/codes/identifier-qr")
	if err != nil || resolved.ID != "identifier-qr" {
		t.Fatalf("generated QR route did not resolve safely: %#v err=%v", resolved, err)
	}
}

func TestRenderersIncludeTheSameAllowedFieldsAndRejectUnsupportedText(t *testing.T) {
	template, err := labelTemplateFromPattern(labelPatternFixture(t, SymbologyCode128))
	if err != nil {
		t.Fatal(err)
	}
	record := LabelRecord{
		IdentifierID: "renderer-parity", EncodedPayload: "LAB-001", HumanReadable: "LAB-001",
		AssetName: "Lab server", AssetTag: "TAG-001", Branding: "StewardMesh",
	}
	for _, renderer := range DefaultLabelRenderers() {
		contents, err := renderer.Render(context.Background(), template, []LabelRecord{record}, false)
		if err != nil {
			t.Fatalf("render %s: %v", renderer.Output(), err)
		}
		for _, allowed := range []string{"LAB-001", "Lab server", "TAG-001", "StewardMesh"} {
			if !bytes.Contains(contents, []byte(allowed)) {
				t.Fatalf("%s omitted allowed field %q: %s", renderer.Output(), allowed, contents)
			}
		}
		unsupported := record
		unsupported.AssetName = "Caf\u00e9 server"
		if _, err := renderer.Render(context.Background(), template, []LabelRecord{unsupported}, false); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("%s silently substituted unsupported label text: %v", renderer.Output(), err)
		}
	}
}

func TestSVGIsSinglePageWhilePDFAndZPLPreserveBoundedBatchGeometry(t *testing.T) {
	labels, _ := newLabelTestService(t)
	input := LabelBatchInput{
		TemplateID: "builtin-atlas-label-code128", TemplateVersion: 1,
		IdentifierIDs: []string{"identifier-code", "identifier-code-two"}, TestPrint: true,
	}
	input.IdempotencyKey, input.Output = "multi-svg", LabelOutputSVG
	if _, _, err := labels.CreateBatch(context.Background(), input); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("expected multi-label SVG rejection, got %v", err)
	}
	input.IdempotencyKey, input.Output = "multi-pdf", LabelOutputPDF
	pdf, created, err := labels.CreateBatch(context.Background(), input)
	if err != nil || !created || pdf.ItemCount != 2 || bytes.Count(pdf.Contents, []byte("/Type /Page ")) != 2 || bytes.Count(pdf.Contents, []byte("/MediaBox [0 0 198.425 85.039]")) != 2 {
		t.Fatalf("expected two physical PDF pages, created=%t err=%v\n%s", created, err, pdf.Contents)
	}
	input.IdempotencyKey, input.Output = "multi-zpl", LabelOutputZPL
	zpl, created, err := labels.CreateBatch(context.Background(), input)
	if err != nil || !created || zpl.ItemCount != 2 || bytes.Count(zpl.Contents, []byte("^XA\n")) != 2 || bytes.Count(zpl.Contents, []byte("^PW560\n^LL240\n")) != 2 {
		t.Fatalf("expected two fixed-size ZPL labels, created=%t err=%v\n%s", created, err, zpl.Contents)
	}
}

func TestTestPrintCalibrationStaysOutsideSymbolQuietZones(t *testing.T) {
	codeTemplate, err := labelTemplateFromPattern(labelPatternFixture(t, SymbologyCode128))
	if err != nil {
		t.Fatal(err)
	}
	record := LabelRecord{IdentifierID: "calibration", EncodedPayload: "LAB-001", HumanReadable: "LAB-001"}
	svg, err := (SVGLabelRenderer{}).Render(context.Background(), codeTemplate, []LabelRecord{record}, true)
	if err != nil || !bytes.Contains(svg, []byte(`x="0.25" y="0.25"`)) || !bytes.Contains(svg, []byte(`stroke-dasharray="1 1"`)) || !bytes.Contains(svg, []byte(`x="6.0000"`)) || bytes.Contains(svg, []byte("TEST")) {
		t.Fatalf("SVG calibration marker entered label content or quiet zone: err=%v %s", err, svg)
	}
	pdf, err := (PDFLabelRenderer{}).Render(context.Background(), codeTemplate, []LabelRecord{record}, true)
	if err != nil || !bytes.Contains(pdf, []byte("[2 2] 0 d\n0.5 0.5 197.425 84.039 re S\n[] 0 d")) || bytes.Contains(pdf, []byte("TEST")) {
		t.Fatalf("PDF calibration marker is not an outer border: err=%v %s", err, pdf)
	}
	zpl, err := (ZPLLabelRenderer{DotsPerMillimeter: 8}).Render(context.Background(), codeTemplate, []LabelRecord{record}, true)
	if err != nil || !bytes.Contains(zpl, []byte("^FO2,2^GB556,236,2,B,0^FS")) || !bytes.Contains(zpl, []byte("^FO48,")) || bytes.Contains(zpl, []byte("TEST")) {
		t.Fatalf("ZPL calibration marker entered label content or quiet zone: err=%v %s", err, zpl)
	}
}

func TestZPLAdapterValidatesSupportedDeviceDensity(t *testing.T) {
	template, err := labelTemplateFromPattern(labelPatternFixture(t, SymbologyCode128))
	if err != nil {
		t.Fatal(err)
	}
	record := LabelRecord{IdentifierID: "density", EncodedPayload: "LAB-001", HumanReadable: "LAB-001"}
	contents, err := (ZPLLabelRenderer{DotsPerMillimeter: 12}).Render(context.Background(), template, []LabelRecord{record}, false)
	if err != nil || !bytes.Contains(contents, []byte("^PW840\n^LL360\n")) || !bytes.Contains(contents, []byte("^FO72,")) {
		t.Fatalf("12 dpmm geometry was not preserved: err=%v %s", err, contents)
	}
	if _, err := (ZPLLabelRenderer{DotsPerMillimeter: 6}).Render(context.Background(), template, []LabelRecord{record}, false); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("expected unsupported device density rejection, got %v", err)
	}
}

func TestGeneratedQRRoutePrefixCannotBeClaimedAsRawAssociationData(t *testing.T) {
	_, identifiers := newLabelTestService(t)
	ctx := foundation.WithScope(context.Background(), foundation.Scope{OrganizationID: "org-one", ActorID: "operator-one", CorrelationID: "reserved-route-test"})
	if _, _, err := identifiers.CreateIdentifier(ctx, CreateIdentifierInput{ID: "reserved-route", AssetID: "asset-one", Symbology: SymbologyQR, Value: "/atlas/codes/another-id"}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("expected reserved generated-route prefix to be rejected, got %v", err)
	}
	if _, _, err := identifiers.ReplaceIdentifier(ctx, ReplaceIdentifierInput{AssetID: "asset-one", IdentifierID: "identifier-qr", Revision: 1, ReplacementID: "reserved-replacement", ReplacementSymbology: SymbologyQR, ReplacementValue: "/atlas/codes/replacement"}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("expected reserved replacement route to be rejected, got %v", err)
	}
}

func TestCode128AndQRFixturesIncludeScannableStructuresAndQuietZones(t *testing.T) {
	modules, err := code128Modules("A")
	if err != nil || len(modules) != 66 {
		t.Fatalf("unexpected Code 128-B module stream: len=%d err=%v", len(modules), err)
	}
	for index := 0; index < 10; index++ {
		if modules[index] || modules[len(modules)-1-index] {
			t.Fatal("Code 128 quiet zones must remain white")
		}
	}
	matrix, err := qrMatrix("/atlas/codes/identifier-qr")
	if err != nil || len(matrix) < 29 {
		t.Fatalf("unexpected QR matrix: size=%d err=%v", len(matrix), err)
	}
	for index := 0; index < 4; index++ {
		for column := range matrix[index] {
			if matrix[index][column] || matrix[len(matrix)-1-index][column] || matrix[column][index] || matrix[column][len(matrix)-1-index] {
				t.Fatal("QR quiet zone must remain four white modules")
			}
		}
	}
}

func TestZPLAdapterMatchesDeviceFixturesAndEscapesControlLanguage(t *testing.T) {
	codeTemplate, _ := labelTemplateFromPattern(labelPatternFixture(t, SymbologyCode128))
	qrTemplate, _ := labelTemplateFromPattern(labelPatternFixture(t, SymbologyQR))
	fixtures := []struct {
		name     string
		template LabelTemplate
		record   LabelRecord
	}{
		{"code128-v1.zpl", codeTemplate, LabelRecord{IdentifierID: "identifier-code", EncodedPayload: "LAB-001", HumanReadable: "LAB-001", AssetName: "Lab server"}},
		{"qr-v1.zpl", qrTemplate, LabelRecord{IdentifierID: "identifier-qr", EncodedPayload: "/atlas/codes/identifier-qr", HumanReadable: "Visible QR", AssetName: "Lab server"}},
	}
	for _, fixture := range fixtures {
		got, err := (ZPLLabelRenderer{DotsPerMillimeter: 8}).Render(context.Background(), fixture.template, []LabelRecord{fixture.record}, false)
		if err != nil {
			t.Fatalf("render %s: %v", fixture.name, err)
		}
		want, err := os.ReadFile("testdata/zpl/" + fixture.name)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, want) {
			t.Fatalf("ZPL fixture %s changed\nwant:\n%s\ngot:\n%s", fixture.name, want, got)
		}
	}
	escaped, err := (ZPLLabelRenderer{DotsPerMillimeter: 8}).Render(context.Background(), codeTemplate, []LabelRecord{{IdentifierID: "escaped", EncodedPayload: "ABC^XZ", HumanReadable: "^XA~JA_"}}, false)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(escaped, []byte("^FDABC^XZ^FS")) || bytes.Contains(escaped, []byte("^FD^XA~JA_^FS")) || !bytes.Contains(escaped, []byte("ABC_5EXZ")) {
		t.Fatalf("ZPL control characters were not escaped: %s", escaped)
	}
}

func TestLabelValidationRejectsTextAndSymbolOverflow(t *testing.T) {
	labels, _ := newLabelTestService(t)
	custom := labelPatternFixture(t, SymbologyCode128)
	custom.ID, custom.Name = "custom--overflow", "Overflow"
	labels.templates = labelPatternReader{templates: []patterns.Template{custom}}
	_, _, err := labels.CreateBatch(context.Background(), LabelBatchInput{IdempotencyKey: "overflow-label", TemplateID: "custom--overflow", TemplateVersion: 1, IdentifierIDs: []string{"identifier-code"}, Output: LabelOutputSVG})
	if err != nil {
		// The normal fixture fits; this protects against accidental over-strict validation.
		t.Fatalf("valid label unexpectedly overflowed: %v", err)
	}
	codeTemplate, _ := labelTemplateFromPattern(labelPatternFixture(t, SymbologyCode128))
	_, err = (SVGLabelRenderer{}).Render(context.Background(), codeTemplate, []LabelRecord{{IdentifierID: "long", EncodedPayload: strings.Repeat("A", 128), HumanReadable: strings.Repeat("A", 70)}}, false)
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("expected physical overflow rejection, got %v", err)
	}
	tooShort := labelPatternFixture(t, SymbologyCode128)
	for index := range tooShort.Fields {
		if tooShort.Fields[index].Key == "heightMm" {
			height := 18.0
			tooShort.Fields[index].Minimum = &height
			tooShort.Fields[index].Maximum = &height
		}
	}
	if _, err := labelTemplateFromPattern(tooShort); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("expected template-level vertical geometry rejection, got %v", err)
	}
}

func TestLabelArtifactCacheIsByteBoundedAndRetainsConflictDigests(t *testing.T) {
	labels, _ := newLabelTestService(t)
	labels.renderers[LabelOutputSVG] = fixedSizeLabelRenderer{size: 100 << 10}
	for index := 0; index < 90; index++ {
		_, created, err := labels.CreateBatch(context.Background(), LabelBatchInput{
			IdempotencyKey: fmt.Sprintf("bounded-cache-%d", index), TemplateID: "builtin-atlas-label-code128", TemplateVersion: 1,
			IdentifierIDs: []string{"identifier-code"}, Output: LabelOutputSVG,
		})
		if err != nil || !created {
			t.Fatalf("create cache entry %d: created=%t err=%v", index, created, err)
		}
	}
	if labels.batchBytes > maximumCachedBatchBytes || len(labels.batches) >= 90 || len(labels.requestDigests) != 90 {
		t.Fatalf("artifact cache bounds failed: bytes=%d batches=%d digests=%d", labels.batchBytes, len(labels.batches), len(labels.requestDigests))
	}
	if _, cached := labels.batches["bounded-cache-0"]; cached {
		t.Fatal("oldest artifact should have been evicted by the byte bound")
	}
	if _, _, err := labels.CreateBatch(context.Background(), LabelBatchInput{
		IdempotencyKey: "bounded-cache-0", TemplateID: "builtin-atlas-label-code128", TemplateVersion: 1,
		IdentifierIDs: []string{"identifier-code"}, Output: LabelOutputPDF,
	}); !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("expected remembered intent conflict after artifact eviction, got %v", err)
	}
	assets := labels.assets.(labelTestAssets)
	changed := assets.items["asset-one"]
	changed.Name = "Caf\u00e9 changed after preview"
	assets.items["asset-one"] = changed
	batch, created, err := labels.CreateBatch(context.Background(), LabelBatchInput{
		IdempotencyKey: "bounded-cache-0", TemplateID: "builtin-atlas-label-code128", TemplateVersion: 1,
		IdentifierIDs: []string{"identifier-code"}, Output: LabelOutputSVG,
	})
	if err != nil || created || len(batch.Contents) != 100<<10 || !bytes.HasPrefix(batch.Contents, []byte("Lab server")) || labels.batchBytes > maximumCachedBatchBytes {
		t.Fatalf("expected deterministic logical replay after artifact eviction: created=%t bytes=%d cache=%d err=%v", created, len(batch.Contents), labels.batchBytes, err)
	}
	changed.Name = "Lab server"
	assets.items["asset-one"] = changed
	for index := 90; index < 90+maximumCachedBatches; index++ {
		if _, _, err := labels.CreateBatch(context.Background(), LabelBatchInput{
			IdempotencyKey: fmt.Sprintf("bounded-cache-%d", index), TemplateID: "builtin-atlas-label-code128", TemplateVersion: 1,
			IdentifierIDs: []string{"identifier-code"}, Output: LabelOutputSVG,
		}); err != nil {
			t.Fatalf("advance bounded replay window %d: %v", index, err)
		}
	}
	if _, _, err := labels.CreateBatch(context.Background(), LabelBatchInput{
		IdempotencyKey: "bounded-cache-0", TemplateID: "builtin-atlas-label-code128", TemplateVersion: 1,
		IdentifierIDs: []string{"identifier-code"}, Output: LabelOutputSVG,
	}); !errors.Is(err, ErrIdempotencyExpired) {
		t.Fatalf("expected explicit expiry after bounded snapshot eviction, got %v", err)
	}
}

func TestLabelAuthorizationIsRecheckedBeforeCachedBytesAreReturned(t *testing.T) {
	labels, _ := newLabelTestService(t)
	input := LabelBatchInput{
		IdempotencyKey: "authorization-recheck", TemplateID: "builtin-atlas-label-code128", TemplateVersion: 1,
		IdentifierIDs: []string{"identifier-code"}, Output: LabelOutputSVG,
	}
	calls := 0
	batch, created, err := labels.CreateBatchAuthorized(context.Background(), input, func(_ context.Context, asset domain.Asset) error {
		calls++
		if asset.ID != "asset-one" {
			t.Fatalf("unexpected authorized asset: %#v", asset)
		}
		return nil
	})
	if err != nil || !created || len(batch.Contents) == 0 || calls != 1 {
		t.Fatalf("initial authorized render: created=%t calls=%d err=%v", created, calls, err)
	}
	denied := errors.New("asset permission changed")
	if _, _, err := labels.CreateBatchAuthorized(context.Background(), input, func(context.Context, domain.Asset) error {
		calls++
		return denied
	}); !errors.Is(err, denied) || calls != 2 {
		t.Fatalf("cached bytes bypassed current authorization: calls=%d err=%v", calls, err)
	}
}
