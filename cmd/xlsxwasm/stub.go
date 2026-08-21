//go:build !js || !wasm

package main

// The real entry point is built with GOOS=js GOARCH=wasm. This stub keeps
// `go test ./...` from failing on the host because the WASM file is excluded.
func main() {}
