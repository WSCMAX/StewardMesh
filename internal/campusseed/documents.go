package campusseed

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"io"
	"strings"

	"github.com/maxlemke/stewardmesh/internal/storage"
)

func (s *Seeder) seedDocuments(ctx context.Context) error {
	documents := []struct {
		slug      string
		name      string
		mediaType string
		content   []byte
	}{
		{"po-hardware-pdf", "Riverside lab hardware purchase.pdf", "application/pdf", examplePDF("Lab hardware purchase", "One vendor purchase order covering a single instructional lab at Riverside Community College.")},
		{"po-laptops-pdf", "Faculty laptop refresh.pdf", "application/pdf", examplePDF("Faculty laptop refresh", "Faculty and staff laptops purchased from one vendor, without mixing in lab or office fleets.")},
		{"po-accessories-pdf", "Accessories and docks.pdf", "application/pdf", examplePDF("Accessories", "Monitors, Logitech MK270 combos, and Dell WD19 docks included on the same hardware purchase order as the computers they ship with.")},
		{"packing-slip-png", "Packing-slip-lab-hardware.png", "image/png", examplePNG("Riverside packing slip", "Lab hardware received")},
		{"asset-photo-png", "Lab-station-photo.png", "image/png", examplePNG("Studio Arts Mac Lab", "Station photo")},
		{"m365-contract-docx", "Microsoft-365-A3-volume-agreement.docx", "application/vnd.openxmlformats-officedocument.wordprocessingml.document", exampleDOCX("Microsoft 365 A3 Volume Agreement", "Campus-wide volume licensing for employees and students. Annual per-seat cost is recorded in Ledger and shown on Stack licenses.")},
		{"adobe-contract-html", "Adobe-Creative-Cloud-campus-contract.html", "text/html", exampleHTML("Adobe Creative Cloud campus contract", "All Apps volume license for staff and visual arts students.")},
		{"autodesk-quote-pdf", "Autodesk-education-lab-quote.pdf", "application/pdf", examplePDF("Autodesk Education Lab Pack", "Device entitlement covering AutoCAD, Revit, and Maya in instructional labs.")},
		{"warranty-pdf", "Campus-hardware-warranty.pdf", "application/pdf", examplePDF("Hardware warranty addendum", "Three-year warranty covering desktops, laptops, docks, and monitors purchased in FY2026.")},
	}
	for _, document := range documents {
		created, err := s.vault.CreateBlob(ctx, storage.CreateBlobInput{
			Name: document.name, MediaType: document.mediaType,
			SourceSystemID: SourceSystemID, SourceRecordID: document.slug,
			ResourceType: "campus.demo", ResourceID: document.slug,
			Content: bytes.NewReader(document.content),
		})
		if err != nil {
			blobs, listErr := s.vault.ListBlobs(ctx)
			if listErr != nil {
				return fmt.Errorf("create campus document %q: %w", document.name, err)
			}
			matched := false
			for _, blob := range blobs {
				if blob.SourceRecordID == document.slug {
					s.blobIDs[document.slug] = blob.ID
					matched = true
					break
				}
			}
			if !matched {
				return fmt.Errorf("create campus document %q: %w", document.name, err)
			}
			continue
		}
		s.blobIDs[document.slug] = created.ID
	}
	return nil
}

func examplePDF(title, body string) []byte {
	escapedTitle := strings.ReplaceAll(title, "(", "\\(")
	escapedTitle = strings.ReplaceAll(escapedTitle, ")", "\\)")
	escapedBody := strings.ReplaceAll(body, "(", "\\(")
	escapedBody = strings.ReplaceAll(escapedBody, ")", "\\)")
	content := fmt.Sprintf("BT /F1 18 Tf 72 720 Td (%s) Tj T* /F1 12 Tf 0 -28 Td (%s) Tj T* 0 -20 Td (Riverside Community College · Campus demo evidence) Tj ET", escapedTitle, escapedBody)
	objects := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R /Resources << /Font << /F1 5 0 R >> >> >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content),
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
	}
	var builder strings.Builder
	builder.WriteString("%PDF-1.4\n")
	offsets := make([]int, len(objects)+1)
	for index, object := range objects {
		offsets[index+1] = builder.Len()
		fmt.Fprintf(&builder, "%d 0 obj\n%s\nendobj\n", index+1, object)
	}
	startxref := builder.Len()
	fmt.Fprintf(&builder, "xref\n0 %d\n0000000000 65535 f \n", len(objects)+1)
	for index := 1; index <= len(objects); index++ {
		fmt.Fprintf(&builder, "%010d 00000 n \n", offsets[index])
	}
	fmt.Fprintf(&builder, "trailer << /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objects)+1, startxref)
	return []byte(builder.String())
}

func examplePNG(title, subtitle string) []byte {
	canvas := image.NewRGBA(image.Rect(0, 0, 640, 360))
	draw.Draw(canvas, canvas.Bounds(), &image.Uniform{C: color.RGBA{R: 18, G: 42, B: 58, A: 255}}, image.Point{}, draw.Src)
	band := image.Rect(0, 0, 640, 64)
	draw.Draw(canvas, band, &image.Uniform{C: color.RGBA{R: 20, G: 168, B: 148, A: 255}}, image.Point{}, draw.Src)
	_ = title
	_ = subtitle
	var buffer bytes.Buffer
	if err := png.Encode(&buffer, canvas); err != nil {
		return nil
	}
	return buffer.Bytes()
}

func exampleHTML(title, body string) []byte {
	return []byte(`<!DOCTYPE html><html lang="en"><head><meta charset="utf-8"><title>` + title + `</title></head><body><h1>` + title + `</h1><p>` + body + `</p><p>Riverside Community College campus demo contract.</p></body></html>`)
}

func exampleDOCX(title, body string) []byte {
	var buffer bytes.Buffer
	archive := zip.NewWriter(&buffer)
	files := map[string]string{
		"[Content_Types].xml": `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="xml" ContentType="application/xml"/><Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/></Types>`,
		"_rels/.rels":         `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/></Relationships>`,
		"word/document.xml":   fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?><w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body><w:p><w:r><w:t>%s</w:t></w:r></w:p><w:p><w:r><w:t>%s</w:t></w:r></w:p></w:body></w:document>`, xmlEscape(title), xmlEscape(body)),
	}
	for name, contents := range files {
		writer, err := archive.Create(name)
		if err != nil {
			continue
		}
		_, _ = io.WriteString(writer, contents)
	}
	_ = archive.Close()
	return buffer.Bytes()
}

func xmlEscape(value string) string {
	replacer := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;")
	return replacer.Replace(value)
}
