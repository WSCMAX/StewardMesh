// Requirements: REQ-API-001. Feature: integrations.protocols.
package grpcapi

import (
	"bufio"
	"bytes"
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"
)

var openAPIPathPlaceholderPattern = regexp.MustCompile(`\{[^{}]+\}`)

func TestConfiguredRoutesExistInCheckedInOpenAPI(t *testing.T) {
	document, err := os.ReadFile("../../api/openapi/openapi.yaml")
	if err != nil {
		t.Fatal(err)
	}
	operations := openAPIOperations(t, document)

	var missing []string
	for fullMethod, configured := range routes() {
		// There are deliberately no response-kind exclusions. In particular,
		// the authenticated Vault content download is part of the OpenAPI contract.
		operation := normalizedHTTPOperation(configured.method, configured.path)
		if _, exists := operations[operation]; !exists {
			missing = append(missing, fullMethod+" -> "+operation)
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		t.Fatalf("configured gRPC routes missing from api/openapi/openapi.yaml:\n%s", strings.Join(missing, "\n"))
	}
}

func openAPIOperations(t *testing.T, document []byte) map[string]struct{} {
	t.Helper()
	operations := make(map[string]struct{})
	scanner := bufio.NewScanner(bytes.NewReader(document))
	inPaths := false
	currentPath := ""
	for scanner.Scan() {
		line := scanner.Text()
		if line == "paths:" {
			inPaths = true
			continue
		}
		if !inPaths {
			continue
		}
		if line != "" && line[0] != ' ' && line[0] != '#' {
			break
		}
		if strings.HasPrefix(line, "  /") {
			currentPath = strings.TrimSuffix(strings.TrimSpace(line), ":")
			continue
		}
		if currentPath == "" || leadingSpaces(line) != 4 {
			continue
		}
		method := strings.TrimSuffix(strings.TrimSpace(line), ":")
		switch method {
		case "get", "put", "post", "delete", "options", "head", "patch", "trace":
			operations[normalizedHTTPOperation(method, currentPath)] = struct{}{}
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	if len(operations) == 0 {
		t.Fatal("api/openapi/openapi.yaml contains no parsed HTTP operations")
	}
	return operations
}

func normalizedHTTPOperation(method, path string) string {
	return strings.ToUpper(strings.TrimSpace(method)) + " " + openAPIPathPlaceholderPattern.ReplaceAllString(strings.TrimSpace(path), "{}")
}

func leadingSpaces(value string) int {
	return len(value) - len(strings.TrimLeft(value, " "))
}
