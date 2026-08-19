package xlsxexport

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Package xlsxexport builds an Office Open XML workbook from a resolved grid
// sheet. It is compiled to WASM for the browser and tested as ordinary Go.
// Requirement: REQ-WORKSPACE-001. Feature: experience.grid.

type Column struct {
	Key    string  `json:"key"`
	Header string  `json:"header"`
	Kind   string  `json:"kind"`
	Width  float64 `json:"width"`
}

type Sheet struct {
	Name    string     `json:"name"`
	Columns []Column   `json:"columns"`
	Rows    [][]string `json:"rows"`
}

func Build(raw []byte) ([]byte, error) {
	var sheet Sheet
	if err := json.Unmarshal(raw, &sheet); err != nil {
		return nil, fmt.Errorf("invalid sheet: %w", err)
	}
	if strings.TrimSpace(sheet.Name) == "" {
		sheet.Name = "Sheet1"
	}
	if len(sheet.Columns) == 0 {
		return nil, fmt.Errorf("sheet has no columns")
	}
	return Write(sheet)
}

func Write(sheet Sheet) ([]byte, error) {
	buffer := &bytes.Buffer{}
	archive := zip.NewWriter(buffer)
	files := map[string]string{
		"[Content_Types].xml":          contentTypes,
		"_rels/.rels":                  rootRels,
		"xl/_rels/workbook.xml.rels":   workbookRels,
		"xl/workbook.xml":              workbookXML(sheet.Name),
		"xl/styles.xml":                stylesXML,
		"xl/worksheets/sheet1.xml":     worksheetXML(sheet),
	}
	for name, contents := range files {
		entry, err := archive.Create(name)
		if err != nil {
			return nil, err
		}
		if _, err := entry.Write([]byte(contents)); err != nil {
			return nil, err
		}
	}
	if err := archive.Close(); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

func workbookXML(name string) string {
	return `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
		`<workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships">` +
		`<sheets><sheet name="` + xmlEscape(sanitizeSheetName(name)) + `" sheetId="1" r:id="rId1"/></sheets></workbook>`
}

func worksheetXML(sheet Sheet) string {
	columnCount := len(sheet.Columns)
	rowCount := len(sheet.Rows) + 1
	lastCell := cellName(columnCount-1, rowCount-1)
	var builder strings.Builder
	builder.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>`)
	builder.WriteString(`<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main">`)
	builder.WriteString(`<sheetViews><sheetView tabSelected="1" workbookViewId="0"><pane ySplit="1" topLeftCell="A2" activePane="bottomLeft" state="frozen"/></sheetView></sheetViews>`)
	builder.WriteString(`<cols>`)
	for index, column := range sheet.Columns {
		width := column.Width
		if width <= 0 {
			width = 12
		}
		builder.WriteString(`<col min="` + strconv.Itoa(index+1) + `" max="` + strconv.Itoa(index+1) + `" width="` + formatFloat(width) + `" customWidth="1"/>`)
	}
	builder.WriteString(`</cols><sheetData>`)
	builder.WriteString(`<row r="1">`)
	for index, column := range sheet.Columns {
		writeInline(&builder, cellName(index, 0), column.Header, 1)
	}
	builder.WriteString(`</row>`)
	for rowIndex, row := range sheet.Rows {
		builder.WriteString(`<row r="` + strconv.Itoa(rowIndex+2) + `">`)
		for columnIndex, column := range sheet.Columns {
			value := ""
			if columnIndex < len(row) {
				value = row[columnIndex]
			}
			writeValue(&builder, cellName(columnIndex, rowIndex+1), column.Kind, value)
		}
		builder.WriteString(`</row>`)
	}
	builder.WriteString(`</sheetData>`)
	builder.WriteString(`<autoFilter ref="A1:` + lastCell + `"/>`)
	builder.WriteString(`</worksheet>`)
	return builder.String()
}

func writeValue(builder *strings.Builder, reference, kind, value string) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return
	}
	if kind == "number" || kind == "money" {
		if number, err := strconv.ParseFloat(strings.ReplaceAll(trimmed, ",", ""), 64); err == nil {
			style := 0
			if kind == "money" {
				style = 2
			}
			builder.WriteString(`<c r="` + reference + `"`)
			if style > 0 {
				builder.WriteString(` s="` + strconv.Itoa(style) + `"`)
			}
			builder.WriteString(`><v>` + strconv.FormatFloat(number, 'f', -1, 64) + `</v></c>`)
			return
		}
	}
	if kind == "date" || kind == "instant" {
		if serial, ok := excelSerial(trimmed); ok {
			style := 3
			if kind == "instant" {
				style = 4
			}
			builder.WriteString(`<c r="` + reference + `" s="` + strconv.Itoa(style) + `"><v>` + strconv.FormatFloat(serial, 'f', -1, 64) + `</v></c>`)
			return
		}
	}
	writeInline(builder, reference, trimmed, 0)
}

func writeInline(builder *strings.Builder, reference, value string, style int) {
	builder.WriteString(`<c r="` + reference + `" t="inlineStr"`)
	if style > 0 {
		builder.WriteString(` s="` + strconv.Itoa(style) + `"`)
	}
	builder.WriteString(`><is><t xml:space="preserve">` + xmlEscape(value) + `</t></is></c>`)
}

func excelSerial(value string) (float64, bool) {
	layouts := []string{time.RFC3339, "2006-01-02T15:04", "2006-01-02 15:04", "2006-01-02"}
	for _, layout := range layouts {
		parsed, err := time.ParseInLocation(layout, value, time.UTC)
		if err != nil {
			continue
		}
		unix := parsed.UTC().Unix()
		// Excel's day 0 is 1899-12-30, matching the Windows 1900 date system.
		const excelEpoch = -2209161600
		return float64(unix-excelEpoch) / 86400, true
	}
	return 0, false
}

func cellName(column, row int) string {
	return columnLetters(column) + strconv.Itoa(row+1)
}

func columnLetters(index int) string {
	letters := ""
	for index >= 0 {
		letters = string(rune('A'+(index%26))) + letters
		index = index/26 - 1
	}
	return letters
}

func sanitizeSheetName(name string) string {
	trimmed := strings.TrimSpace(name)
	trimmed = strings.NewReplacer(":", " ", "/", " ", "\\", " ", "?", " ", "*", " ", "[", " ", "]", " ").Replace(trimmed)
	if trimmed == "" {
		return "Sheet1"
	}
	runes := []rune(trimmed)
	if len(runes) > 31 {
		return string(runes[:31])
	}
	return trimmed
}

func xmlEscape(value string) string {
	var builder strings.Builder
	if err := xml.EscapeText(&builder, []byte(value)); err != nil {
		return ""
	}
	return builder.String()
}

func formatFloat(value float64) string {
	return strconv.FormatFloat(value, 'f', 2, 64)
}

const contentTypes = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
	`<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">` +
	`<Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>` +
	`<Default Extension="xml" ContentType="application/xml"/>` +
	`<Override PartName="/xl/workbook.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml"/>` +
	`<Override PartName="/xl/worksheets/sheet1.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml"/>` +
	`<Override PartName="/xl/styles.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.styles+xml"/>` +
	`</Types>`

const rootRels = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
	`<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
	`<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="xl/workbook.xml"/>` +
	`</Relationships>`

const workbookRels = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
	`<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
	`<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/sheet1.xml"/>` +
	`<Relationship Id="rId2" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/styles" Target="styles.xml"/>` +
	`</Relationships>`

const stylesXML = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
	`<styleSheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main">` +
	`<fonts count="2"><font><sz val="11"/><name val="Calibri"/></font><font><b/><sz val="11"/><name val="Calibri"/></font></fonts>` +
	`<fills count="1"><fill><patternFill patternType="none"/></fill></fills>` +
	`<borders count="1"><border><left/><right/><top/><bottom/><diagonal/></border></borders>` +
	`<cellStyleXfs count="1"><xf/></cellStyleXfs>` +
	`<cellXfs count="5">` +
	`<xf numFmtId="0" fontId="0" fillId="0" borderId="0"/>` +
	`<xf numFmtId="0" fontId="1" fillId="0" borderId="0" applyFont="1"/>` +
	`<xf numFmtId="7" fontId="0" fillId="0" borderId="0" applyNumberFormat="1"/>` +
	`<xf numFmtId="14" fontId="0" fillId="0" borderId="0" applyNumberFormat="1"/>` +
	`<xf numFmtId="22" fontId="0" fillId="0" borderId="0" applyNumberFormat="1"/>` +
	`</cellXfs>` +
	`</styleSheet>`
