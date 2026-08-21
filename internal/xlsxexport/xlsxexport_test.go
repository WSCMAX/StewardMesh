package xlsxexport

import (
	"archive/zip"
	"bytes"
	"strings"
	"testing"
)

func TestWriteProducesWorkbookWithFrozenHeader(t *testing.T) {
	document, err := Write(Sheet{
		Name: "Asset inventory",
		Columns: []Column{
			{Key: "name", Header: "Asset name", Kind: "text", Width: 15},
			{Key: "seats", Header: "Seats", Kind: "number", Width: 8},
			{Key: "cost", Header: "Unit cost", Kind: "money", Width: 8},
		},
		Rows: [][]string{
			{"Lab server", "3", "12.50"},
			{"Lab printer", "1", ""},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	reader, err := zip.NewReader(bytes.NewReader(document), int64(len(document)))
	if err != nil {
		t.Fatal(err)
	}
	contents := map[string]string{}
	for _, file := range reader.File {
		body, err := file.Open()
		if err != nil {
			t.Fatal(err)
		}
		buffer := &bytes.Buffer{}
		if _, err := buffer.ReadFrom(body); err != nil {
			t.Fatal(err)
		}
		_ = body.Close()
		contents[file.Name] = buffer.String()
	}
	sheet := contents["xl/worksheets/sheet1.xml"]
	if sheet == "" {
		t.Fatal("expected worksheet part")
	}
	for _, fragment := range []string{
		`state="frozen"`,
		`<autoFilter ref="A1:C3"/>`,
		`Lab server`,
		`<v>3</v>`,
		`<v>12.5</v>`,
		`s="2"`,
	} {
		if !strings.Contains(sheet, fragment) {
			t.Fatalf("worksheet missing %q in %s", fragment, sheet)
		}
	}
}

func TestBuildRejectsEmptyColumns(t *testing.T) {
	if _, err := Build([]byte(`{"name":"Empty","columns":[],"rows":[]}`)); err == nil {
		t.Fatal("expected empty columns to fail")
	}
}

func TestColumnLetters(t *testing.T) {
	if got := columnLetters(0); got != "A" {
		t.Fatalf("got %s", got)
	}
	if got := columnLetters(25); got != "Z" {
		t.Fatalf("got %s", got)
	}
	if got := columnLetters(26); got != "AA" {
		t.Fatalf("got %s", got)
	}
}
