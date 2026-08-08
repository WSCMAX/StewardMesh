// Package traceability verifies that implementation artifacts remain linked
// to their requirements and canonical feature IDs.
// Requirement: REQ-FOUNDATION-001.
package traceability

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

var requirementIDPattern = regexp.MustCompile(`^((REQ|SEC)-[A-Z0-9]+-[0-9]{3}|(A11Y|DOC)-[0-9]{3})$`)

var requiredArtifactKinds = []string{"api", "code", "documentation", "schema", "tests", "ui"}

type Manifest struct {
	Entries []Entry `json:"entries"`
}

type Entry struct {
	RequirementID string              `json:"requirementId"`
	FeatureID     string              `json:"featureId"`
	Artifacts     map[string][]string `json:"artifacts"`
}

func Verify(root, manifestPath string) error {
	root, err := filepath.Abs(root)
	if err != nil {
		return fmt.Errorf("resolve repository root: %w", err)
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return fmt.Errorf("resolve repository root symlinks: %w", err)
	}
	resolvedManifest, err := resolveInsideRoot(root, manifestPath)
	if err != nil {
		return fmt.Errorf("resolve traceability manifest: %w", err)
	}
	manifestData, err := os.ReadFile(resolvedManifest)
	if err != nil {
		return fmt.Errorf("read traceability manifest: %w", err)
	}
	var manifest Manifest
	decoder := json.NewDecoder(bytes.NewReader(manifestData))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&manifest); err != nil {
		return fmt.Errorf("decode traceability manifest: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("traceability manifest must contain exactly one JSON document")
	}
	if len(manifest.Entries) == 0 {
		return errors.New("traceability manifest must contain at least one entry")
	}

	requirementsPath, err := resolveInsideRoot(root, "docs/requirements/initial.md")
	if err != nil {
		return fmt.Errorf("resolve requirements catalog: %w", err)
	}
	requirementsData, err := os.ReadFile(requirementsPath)
	if err != nil {
		return fmt.Errorf("read requirements catalog: %w", err)
	}
	featuresPath, err := resolveInsideRoot(root, "docs/features/dictionary.md")
	if err != nil {
		return fmt.Errorf("resolve feature dictionary: %w", err)
	}
	featuresData, err := os.ReadFile(featuresPath)
	if err != nil {
		return fmt.Errorf("read feature dictionary: %w", err)
	}

	seenRequirements := make(map[string]struct{}, len(manifest.Entries))
	var problems []error
	for _, entry := range manifest.Entries {
		if !requirementIDPattern.MatchString(entry.RequirementID) {
			problems = append(problems, fmt.Errorf("invalid requirement id %q", entry.RequirementID))
			continue
		}
		if _, duplicate := seenRequirements[entry.RequirementID]; duplicate {
			problems = append(problems, fmt.Errorf("duplicate requirement id %q", entry.RequirementID))
			continue
		}
		seenRequirements[entry.RequirementID] = struct{}{}
		if strings.TrimSpace(entry.FeatureID) == "" {
			problems = append(problems, fmt.Errorf("%s has no canonical feature id", entry.RequirementID))
		}
		if !bytes.Contains(requirementsData, []byte(entry.RequirementID)) {
			problems = append(problems, fmt.Errorf("%s is missing from the requirements catalog", entry.RequirementID))
		}
		if !bytes.Contains(featuresData, []byte(entry.FeatureID)) {
			problems = append(problems, fmt.Errorf("%s is missing from the feature dictionary", entry.FeatureID))
		}
		for _, kind := range requiredArtifactKinds {
			paths := entry.Artifacts[kind]
			if len(paths) == 0 {
				problems = append(problems, fmt.Errorf("%s has no %s artifacts", entry.RequirementID, kind))
				continue
			}
			sort.Strings(paths)
			for _, path := range paths {
				if err := verifyArtifact(root, path, entry); err != nil {
					problems = append(problems, err)
				}
			}
		}
	}
	return errors.Join(problems...)
}

func verifyArtifact(root, path string, entry Entry) error {
	cleanPath := filepath.Clean(path)
	if filepath.IsAbs(cleanPath) || cleanPath == ".." || strings.HasPrefix(cleanPath, ".."+string(filepath.Separator)) {
		return fmt.Errorf("%s contains unsafe artifact path %q", entry.RequirementID, path)
	}
	absolutePath, err := resolveInsideRoot(root, cleanPath)
	if err != nil {
		return fmt.Errorf("%s artifact %q is unsafe: %w", entry.RequirementID, path, err)
	}
	contents, err := os.ReadFile(absolutePath)
	if err != nil {
		return fmt.Errorf("%s artifact %q cannot be read: %w", entry.RequirementID, path, err)
	}
	if !bytes.Contains(contents, []byte(entry.RequirementID)) && !bytes.Contains(contents, []byte(entry.FeatureID)) {
		return fmt.Errorf("%s artifact %q contains neither the requirement nor canonical feature id", entry.RequirementID, path)
	}
	return nil
}

func resolveInsideRoot(root, path string) (string, error) {
	cleanPath := filepath.Clean(path)
	if filepath.IsAbs(cleanPath) || cleanPath == ".." || strings.HasPrefix(cleanPath, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path %q leaves the repository root", path)
	}
	resolved, err := filepath.EvalSymlinks(filepath.Join(root, cleanPath))
	if err != nil {
		return "", err
	}
	relative, err := filepath.Rel(root, resolved)
	if err != nil {
		return "", err
	}
	if relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path %q resolves outside the repository root", path)
	}
	return resolved, nil
}
