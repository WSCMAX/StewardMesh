package httpapi

import (
	"net/http"
	"strings"
	"time"

	"github.com/maxlemke/stewardmesh/internal/foundation"
	"github.com/maxlemke/stewardmesh/internal/guard"
	"github.com/maxlemke/stewardmesh/internal/labels"
	"github.com/maxlemke/stewardmesh/internal/people"
)

func (s *Server) listCheckoutGroups(w http.ResponseWriter, r *http.Request, authentication guard.Authentication) {
	if s.people == nil {
		writeError(w, r, http.StatusServiceUnavailable, "repository_unavailable", "people directory unavailable")
		return
	}
	visibility, ok := s.directoryVisibility(w, r, authentication)
	if !ok {
		return
	}
	items, err := s.people.ListCheckoutGroups(r.Context(), visibility)
	if err != nil {
		writePeopleError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) createCheckoutGroup(w http.ResponseWriter, r *http.Request, authentication guard.Authentication) {
	if s.people == nil {
		writeError(w, r, http.StatusServiceUnavailable, "repository_unavailable", "people directory unavailable")
		return
	}
	var input people.CreateCheckoutGroupInput
	if err := decodeJSON(w, r, 32<<10, &input); err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_request", "invalid checkout group payload")
		return
	}
	created, err := s.people.CreateCheckoutGroup(r.Context(), input)
	if err != nil {
		writePeopleError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, created)
}

func (s *Server) addCheckoutGroupMember(w http.ResponseWriter, r *http.Request, _ guard.Authentication) {
	if s.people == nil {
		writeError(w, r, http.StatusServiceUnavailable, "repository_unavailable", "people directory unavailable")
		return
	}
	var input struct {
		IdentityID string `json:"identityId"`
	}
	if err := decodeJSON(w, r, 16<<10, &input); err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_request", "invalid checkout group member payload")
		return
	}
	updated, err := s.people.AddCheckoutGroupMember(r.Context(), r.PathValue("groupID"), input.IdentityID)
	if err != nil {
		writePeopleError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

func (s *Server) listBulkCheckouts(w http.ResponseWriter, r *http.Request, authentication guard.Authentication) {
	if s.people == nil {
		writeError(w, r, http.StatusServiceUnavailable, "repository_unavailable", "people directory unavailable")
		return
	}
	visibility, ok := s.directoryVisibility(w, r, authentication)
	if !ok {
		return
	}
	items, err := s.people.ListBulkCheckouts(r.Context(), visibility)
	if err != nil {
		writePeopleError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) rankCheckoutAvailability(w http.ResponseWriter, r *http.Request, authentication guard.Authentication) {
	if s.people == nil {
		writeError(w, r, http.StatusServiceUnavailable, "repository_unavailable", "people directory unavailable")
		return
	}
	if s.labels == nil {
		writeError(w, r, http.StatusServiceUnavailable, "repository_unavailable", "labels unavailable")
		return
	}
	if !s.requireOrganizationPermission(w, r, authentication, guard.PermissionAssetsRead) {
		return
	}
	visibility, ok := s.directoryVisibility(w, r, authentication)
	if !ok {
		return
	}
	var input struct {
		DefinitionID      string    `json:"definitionId"`
		Value             string    `json:"value"`
		From              time.Time `json:"from"`
		To                time.Time `json:"to"`
		PreferredModelIDs []string  `json:"preferredModelIds"`
		Quantity          int       `json:"quantity"`
		AssetIDs          []string  `json:"assetIds"`
	}
	if err := decodeJSON(w, r, 64<<10, &input); err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_request", "invalid checkout availability payload")
		return
	}
	assetIDs := uniqueNonEmptyStrings(input.AssetIDs)
	if input.DefinitionID != "" {
		assignments, err := s.labels.ListAssignmentsForDefinition(r.Context(), input.DefinitionID, "atlas.asset")
		if err != nil {
			writeLabelsError(w, r, err)
			return
		}
		tagged := make([]string, 0, len(assignments))
		for _, assignment := range assignments {
			if assignment.RecordType != "atlas.asset" {
				continue
			}
			if !labelAssignmentMatches(assignment, input.Value) {
				continue
			}
			tagged = append(tagged, assignment.RecordID)
		}
		assetIDs = tagged
	}
	candidates, err := s.people.RankCheckoutCandidates(r.Context(), people.RankCheckoutInput{
		AssetIDs:          assetIDs,
		From:              input.From,
		To:                input.To,
		PreferredModelIDs: input.PreferredModelIDs,
		Visibility:        visibility,
	})
	if err != nil {
		writePeopleError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": candidates, "quantity": input.Quantity})
}

func (s *Server) createBulkCheckout(w http.ResponseWriter, r *http.Request, authentication guard.Authentication) {
	if s.people == nil {
		writeError(w, r, http.StatusServiceUnavailable, "repository_unavailable", "people directory unavailable")
		return
	}
	if !s.requireOrganizationPermission(w, r, authentication, guard.PermissionAssetsWrite) {
		return
	}
	var input struct {
		AssigneeKind      people.AssigneeKind      `json:"assigneeKind"`
		AssigneeID        string                   `json:"assigneeId"`
		Purpose           people.AssignmentPurpose `json:"purpose"`
		EffectiveFrom     *time.Time               `json:"effectiveFrom"`
		DueAt             *time.Time               `json:"dueAt"`
		EventSummary      string                   `json:"eventSummary"`
		LabelDefinitionID string                   `json:"labelDefinitionId"`
		LabelValue        string                   `json:"labelValue"`
		AssetIDs          []string                 `json:"assetIds"`
		ConflictPolicy    people.ConflictPolicy    `json:"conflictPolicy"`
	}
	if err := decodeJSON(w, r, 64<<10, &input); err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_request", "invalid bulk checkout payload")
		return
	}
	effectiveFrom := time.Time{}
	if input.EffectiveFrom != nil {
		effectiveFrom = input.EffectiveFrom.UTC()
	}
	var dueAt *time.Time
	if input.DueAt != nil {
		normalized := input.DueAt.UTC()
		dueAt = &normalized
	}
	created, assignments, err := s.people.CreateBulkCheckout(r.Context(), people.CreateBulkCheckoutInput{
		AssigneeKind:      input.AssigneeKind,
		AssigneeID:        input.AssigneeID,
		Purpose:           input.Purpose,
		EffectiveFrom:     effectiveFrom,
		DueAt:             dueAt,
		EventSummary:      input.EventSummary,
		LabelDefinitionID: input.LabelDefinitionID,
		LabelValue:        input.LabelValue,
		AssetIDs:          input.AssetIDs,
		ConflictPolicy:    input.ConflictPolicy,
	})
	if err != nil {
		writePeopleError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"bulkCheckout": created, "assignments": assignments})
}

func labelAssignmentMatches(assignment labels.Assignment, value string) bool {
	value = strings.TrimSpace(value)
	if value == "" {
		return true
	}
	if strings.TrimSpace(assignment.ValueText) == value {
		return true
	}
	for _, item := range assignment.Values {
		if item == value {
			return true
		}
	}
	return false
}

func uniqueNonEmptyStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func writePeopleOverlap(w http.ResponseWriter, r *http.Request, overlap *people.OverlapError) {
	correlationID := ""
	if scope, ok := foundation.ScopeFromContext(r.Context()); ok {
		correlationID = scope.CorrelationID
	}
	writeJSON(w, http.StatusConflict, map[string]any{
		"error": map[string]any{
			"code":          "assignment_overlap",
			"message":       overlap.Error(),
			"conflictKind":  overlap.ConflictKind,
			"conflicts":     overlap.Conflicts,
			"correlationId": correlationID,
		},
	})
}
