// Command tracecheck enforces REQ-FOUNDATION-001 artifact traceability in CI.
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/maxlemke/stewardmesh/internal/traceability"
)

func main() {
	root := flag.String("root", ".", "repository root")
	manifest := flag.String("manifest", "docs/requirements/traceability.json", "manifest path relative to the repository root")
	flag.Parse()
	if err := traceability.Verify(*root, *manifest); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println("requirement traceability verified")
}
