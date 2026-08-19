//go:build js && wasm

package main

import (
	"syscall/js"

	"github.com/maxlemke/stewardmesh/internal/xlsxexport"
)

// A tiny WASM host that turns a JSON grid sheet into an .xlsx document.
// Requirement: REQ-WORKSPACE-001. Feature: experience.grid.

func main() {
	js.Global().Set("stewardmeshXlsxExport", js.FuncOf(exportSheet))
	select {}
}

func exportSheet(_ js.Value, args []js.Value) any {
	if len(args) < 1 {
		return jsError("sheet JSON is required")
	}
	document, err := xlsxexport.Build([]byte(args[0].String()))
	if err != nil {
		return jsError(err.Error())
	}
	buffer := js.Global().Get("Uint8Array").New(len(document))
	js.CopyBytesToJS(buffer, document)
	return buffer
}

func jsError(message string) js.Value {
	value := js.Global().Get("Object").New()
	value.Set("error", message)
	return value
}
