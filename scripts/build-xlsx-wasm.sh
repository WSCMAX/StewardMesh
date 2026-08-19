#!/usr/bin/env bash
set -euo pipefail

# Builds the grid XLSX helper as WebAssembly and copies Go's wasm_exec.js
# into the Vite public directory so the browser can host it.
# Requirement: REQ-WORKSPACE-001. Feature: experience.grid.

root="$(cd "$(dirname "$0")/.." && pwd)"
public="$root/web/public"
mkdir -p "$public"

GOOS=js GOARCH=wasm go build -o "$public/xlsx.wasm" "$root/cmd/xlsxwasm"

runtime="$(go env GOROOT)/lib/wasm/wasm_exec.js"
if [[ ! -f "$runtime" ]]; then
  runtime="$(go env GOROOT)/misc/wasm/wasm_exec.js"
fi
cp "$runtime" "$public/wasm_exec.js"
