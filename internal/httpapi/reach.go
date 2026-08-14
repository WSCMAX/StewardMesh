package httpapi

// Requirement: REQ-REACH-001. Feature: messaging.delivery. GitHub: #12.

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/maxlemke/stewardmesh/internal/guard"
	"github.com/maxlemke/stewardmesh/internal/reach"
)

func (s *Server) requireReach(w http.ResponseWriter, r *http.Request) bool {
	s.noStore(w)
	if s.reach == nil {
		writeError(w, r, http.StatusServiceUnavailable, "messaging_unavailable", "Reach messaging is unavailable")
		return false
	}
	return true
}

func (s *Server) listReachEndpoints(w http.ResponseWriter, r *http.Request, _ guard.Authentication) {
	if !s.requireReach(w, r) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": s.reach.ListEndpoints()})
}

func (s *Server) listReachProviders(w http.ResponseWriter, r *http.Request, _ guard.Authentication) {
	if !s.requireReach(w, r) {
		return
	}
	items, err := s.reach.ListProviders(r.Context())
	if err != nil {
		writeReachError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) createReachProvider(w http.ResponseWriter, r *http.Request, _ guard.Authentication) {
	if !s.requireReach(w, r) {
		return
	}
	var input reach.CreateProviderInput
	if err := decodeJSON(w, r, 16<<10, &input); err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_request", "invalid Reach provider payload")
		return
	}
	created, err := s.reach.CreateProvider(r.Context(), input)
	if err != nil {
		writeReachError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, created)
}

func (s *Server) updateReachProvider(w http.ResponseWriter, r *http.Request, _ guard.Authentication) {
	if !s.requireReach(w, r) {
		return
	}
	var input reach.UpdateProviderInput
	if err := decodeJSON(w, r, 8<<10, &input); err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_request", "invalid Reach provider payload")
		return
	}
	updated, err := s.reach.UpdateProvider(r.Context(), r.PathValue("providerID"), input)
	if err != nil {
		writeReachError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

func (s *Server) rotateReachProviderSecret(w http.ResponseWriter, r *http.Request, _ guard.Authentication) {
	if !s.requireReach(w, r) {
		return
	}
	var input reach.RotateSecretInput
	if err := decodeJSON(w, r, 4<<10, &input); err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_request", "invalid Reach secret rotation payload")
		return
	}
	updated, err := s.reach.RotateProviderSecret(r.Context(), r.PathValue("providerID"), input)
	if err != nil {
		writeReachError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

func (s *Server) testReachProvider(w http.ResponseWriter, r *http.Request, _ guard.Authentication) {
	if !s.requireReach(w, r) {
		return
	}
	var input reach.TestProviderInput
	if err := decodeJSON(w, r, 2<<10, &input); err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_request", "invalid Reach provider test payload")
		return
	}
	result, err := s.reach.TestProvider(r.Context(), r.PathValue("providerID"), input)
	if err != nil {
		writeReachError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) listReachProviderTests(w http.ResponseWriter, r *http.Request, _ guard.Authentication) {
	if !s.requireReach(w, r) {
		return
	}
	items, err := s.reach.ListProviderTests(r.Context(), r.PathValue("providerID"))
	if err != nil {
		writeReachError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) listReachTemplates(w http.ResponseWriter, r *http.Request, _ guard.Authentication) {
	if !s.requireReach(w, r) {
		return
	}
	items, err := s.reach.ListTemplates(r.Context())
	if err != nil {
		writeReachError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) createReachTemplate(w http.ResponseWriter, r *http.Request, _ guard.Authentication) {
	if !s.requireReach(w, r) {
		return
	}
	var input reach.CreateTemplateInput
	if err := decodeJSON(w, r, 8<<10, &input); err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_request", "invalid Reach template payload")
		return
	}
	created, err := s.reach.CreateTemplate(r.Context(), input)
	if err != nil {
		writeReachError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, created)
}

func (s *Server) updateReachTemplate(w http.ResponseWriter, r *http.Request, _ guard.Authentication) {
	if !s.requireReach(w, r) {
		return
	}
	var input reach.UpdateTemplateInput
	if err := decodeJSON(w, r, 8<<10, &input); err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_request", "invalid Reach template payload")
		return
	}
	updated, err := s.reach.UpdateTemplate(r.Context(), r.PathValue("templateID"), input)
	if err != nil {
		writeReachError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

func (s *Server) listReachGroups(w http.ResponseWriter, r *http.Request, _ guard.Authentication) {
	if !s.requireReach(w, r) {
		return
	}
	items, err := s.reach.ListGroups(r.Context())
	if err != nil {
		writeReachError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) createReachGroup(w http.ResponseWriter, r *http.Request, _ guard.Authentication) {
	if !s.requireReach(w, r) {
		return
	}
	var input reach.CreateGroupInput
	if err := decodeJSON(w, r, 32<<10, &input); err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_request", "invalid Reach subscriber group payload")
		return
	}
	created, err := s.reach.CreateGroup(r.Context(), input)
	if err != nil {
		writeReachError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, created)
}

func (s *Server) updateReachGroup(w http.ResponseWriter, r *http.Request, _ guard.Authentication) {
	if !s.requireReach(w, r) {
		return
	}
	var input reach.UpdateGroupInput
	if err := decodeJSON(w, r, 32<<10, &input); err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_request", "invalid Reach subscriber group payload")
		return
	}
	updated, err := s.reach.UpdateGroup(r.Context(), r.PathValue("groupID"), input)
	if err != nil {
		writeReachError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

func (s *Server) listReachMessages(w http.ResponseWriter, r *http.Request, _ guard.Authentication) {
	if !s.requireReach(w, r) {
		return
	}
	limit := reach.DefaultMessageLimit
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil {
			writeError(w, r, http.StatusBadRequest, "validation_failed", "Reach message pagination is invalid")
			return
		}
		limit = parsed
	}
	items, err := s.reach.ListMessages(r.Context(), limit)
	if err != nil {
		writeReachError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) sendReachMessage(w http.ResponseWriter, r *http.Request, _ guard.Authentication) {
	if !s.requireReach(w, r) {
		return
	}
	var input reach.SendInput
	if err := decodeJSON(w, r, 16<<10, &input); err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_request", "invalid Reach send payload")
		return
	}
	input.IdempotencyKey = strings.TrimSpace(r.Header.Get(idempotencyHeader))
	message, err := s.reach.Send(r.Context(), input)
	if err != nil {
		writeReachError(w, r, err)
		return
	}
	writeJSON(w, http.StatusAccepted, message)
}

func (s *Server) retryReachMessage(w http.ResponseWriter, r *http.Request, _ guard.Authentication) {
	if !s.requireReach(w, r) {
		return
	}
	var input reach.RetryInput
	if err := decodeJSON(w, r, 2<<10, &input); err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_request", "invalid Reach retry payload")
		return
	}
	message, err := s.reach.Retry(r.Context(), r.PathValue("messageID"), input)
	if err != nil {
		writeReachError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, message)
}

func (s *Server) listReachMessageAttempts(w http.ResponseWriter, r *http.Request, _ guard.Authentication) {
	if !s.requireReach(w, r) {
		return
	}
	items, err := s.reach.ListAttempts(r.Context(), r.PathValue("messageID"))
	if err != nil {
		writeReachError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) processReachSignals(w http.ResponseWriter, r *http.Request, _ guard.Authentication) {
	if !s.requireReach(w, r) {
		return
	}
	var input reach.ProcessSignalsInput
	if err := decodeJSON(w, r, 2<<10, &input); err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_request", "invalid Reach Signals processing payload")
		return
	}
	result, err := s.reach.ProcessSignals(r.Context(), input)
	if err != nil {
		writeReachError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func writeReachError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, guard.ErrResourceWriteLocked):
		writeError(w, r, http.StatusLocked, "ownership_locked", "claim local ownership before changing this imported Reach record")
	case errors.Is(err, reach.ErrInvalidInput):
		writeError(w, r, http.StatusBadRequest, "validation_failed", "Reach request failed validation")
	case errors.Is(err, reach.ErrNotFound):
		writeError(w, r, http.StatusNotFound, "not_found", "Reach record was not found")
	case errors.Is(err, reach.ErrConflict):
		writeError(w, r, http.StatusConflict, "conflict", "Reach record conflicts with current state")
	case errors.Is(err, reach.ErrEndpointUnavailable):
		writeError(w, r, http.StatusServiceUnavailable, "endpoint_unavailable", "Reach endpoint is unavailable")
	default:
		writeError(w, r, http.StatusInternalServerError, "internal_error", "Reach request could not be completed")
	}
}
