package httpapi

// Requirement: REQ-SIGNALS-001. Feature: alerts.rules. GitHub: #11.

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/maxlemke/stewardmesh/internal/guard"
	"github.com/maxlemke/stewardmesh/internal/signals"
)

func (s *Server) listSignalRules(w http.ResponseWriter, r *http.Request, _ guard.Authentication) {
	if !s.requireSignals(w, r) {
		return
	}
	items, err := s.signals.ListRules(r.Context())
	if err != nil {
		writeSignalsError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}
func (s *Server) createSignalRule(w http.ResponseWriter, r *http.Request, _ guard.Authentication) {
	if !s.requireSignals(w, r) {
		return
	}
	var input signals.CreateRuleInput
	if decodeJSON(w, r, 32<<10, &input) != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_request", "invalid Signals rule payload")
		return
	}
	created, err := s.signals.CreateRule(r.Context(), input)
	if err != nil {
		writeSignalsError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, created)
}
func (s *Server) updateSignalRule(w http.ResponseWriter, r *http.Request, _ guard.Authentication) {
	if !s.requireSignals(w, r) {
		return
	}
	var input signals.UpdateRuleInput
	if decodeJSON(w, r, 32<<10, &input) != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_request", "invalid Signals rule payload")
		return
	}
	updated, err := s.signals.UpdateRule(r.Context(), r.PathValue("ruleID"), input)
	if err != nil {
		writeSignalsError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

func (s *Server) listSignalAlerts(w http.ResponseWriter, r *http.Request, _ guard.Authentication) {
	if !s.requireSignals(w, r) {
		return
	}
	query, ok := signalAlertQuery(w, r)
	if !ok {
		return
	}
	items, err := s.signals.ListAlerts(r.Context(), query)
	if err != nil {
		writeSignalsError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}
func (s *Server) listSignalAlertHistory(w http.ResponseWriter, r *http.Request, _ guard.Authentication) {
	if !s.requireSignals(w, r) {
		return
	}
	items, err := s.signals.ListAlertHistory(r.Context(), r.PathValue("alertID"))
	if err != nil {
		writeSignalsError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}
func (s *Server) evaluateSignals(w http.ResponseWriter, r *http.Request, _ guard.Authentication) {
	if !s.requireSignals(w, r) {
		return
	}
	var input struct {
		AsOf string `json:"asOf"`
	}
	if decodeJSON(w, r, 8<<10, &input) != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_request", "invalid Signals evaluation payload")
		return
	}
	var asOf time.Time
	var err error
	if strings.TrimSpace(input.AsOf) != "" {
		asOf, err = time.Parse(time.RFC3339, input.AsOf)
		if err != nil {
			writeError(w, r, http.StatusBadRequest, "validation_failed", "Signals evaluation time is invalid")
			return
		}
	}
	result, err := s.signals.Evaluate(r.Context(), asOf)
	if err != nil {
		writeSignalsError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}
func (s *Server) acknowledgeSignalAlert(w http.ResponseWriter, r *http.Request, _ guard.Authentication) {
	if !s.requireSignals(w, r) {
		return
	}
	var input signals.RevisionInput
	if decodeJSON(w, r, 8<<10, &input) != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_request", "invalid Signals acknowledgment payload")
		return
	}
	updated, err := s.signals.Acknowledge(r.Context(), r.PathValue("alertID"), input.Revision)
	if err != nil {
		writeSignalsError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, updated)
}
func (s *Server) assignSignalAlert(w http.ResponseWriter, r *http.Request, _ guard.Authentication) {
	if !s.requireSignals(w, r) {
		return
	}
	var input signals.AssignmentInput
	if decodeJSON(w, r, 8<<10, &input) != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_request", "invalid Signals assignment payload")
		return
	}
	updated, err := s.signals.Assign(r.Context(), r.PathValue("alertID"), input)
	if err != nil {
		writeSignalsError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

func (s *Server) listSignalSubscriptions(w http.ResponseWriter, r *http.Request, _ guard.Authentication) {
	if !s.requireSignals(w, r) {
		return
	}
	items, err := s.signals.ListSubscriptions(r.Context())
	if err != nil {
		writeSignalsError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}
func (s *Server) listSignalSubscriptionTargets(w http.ResponseWriter, r *http.Request, _ guard.Authentication) {
	if !s.requireSignals(w, r) {
		return
	}
	items, err := s.signals.ListSubscriptionTargets(r.Context())
	if err != nil {
		writeSignalsError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}
func (s *Server) createSignalSubscription(w http.ResponseWriter, r *http.Request, _ guard.Authentication) {
	if !s.requireSignals(w, r) {
		return
	}
	var input signals.CreateSubscriptionInput
	if decodeJSON(w, r, 8<<10, &input) != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_request", "invalid Signals subscription payload")
		return
	}
	created, err := s.signals.CreateSubscription(r.Context(), input)
	if err != nil {
		writeSignalsError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, created)
}
func (s *Server) deleteSignalSubscription(w http.ResponseWriter, r *http.Request, _ guard.Authentication) {
	if !s.requireSignals(w, r) {
		return
	}
	deleted, err := s.signals.DeleteSubscription(r.Context(), r.PathValue("subscriptionID"))
	if err != nil {
		writeSignalsError(w, r, err)
		return
	}
	if !deleted {
		writeError(w, r, http.StatusNotFound, "not_found", "Signals subscription was not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) listPendingSignalDeliveries(w http.ResponseWriter, r *http.Request, _ guard.Authentication) {
	if !s.requireSignals(w, r) {
		return
	}
	var asOf time.Time
	if raw := strings.TrimSpace(r.URL.Query().Get("asOf")); raw != "" {
		parsed, err := time.Parse(time.RFC3339Nano, raw)
		if err != nil {
			writeError(w, r, http.StatusBadRequest, "validation_failed", "Signals delivery time is invalid")
			return
		}
		asOf = parsed.UTC()
	}
	limit := 0
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil {
			writeError(w, r, http.StatusBadRequest, "validation_failed", "Signals delivery limit is invalid")
			return
		}
		limit = parsed
	}
	items, err := s.signals.ListPendingDeliveries(r.Context(), asOf, limit)
	if err != nil {
		writeSignalsError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) recordSignalDeliveryAttempt(w http.ResponseWriter, r *http.Request, _ guard.Authentication) {
	if !s.requireSignals(w, r) {
		return
	}
	var input struct {
		Succeeded bool   `json:"succeeded"`
		Retryable bool   `json:"retryable"`
		ErrorCode string `json:"errorCode"`
	}
	if decodeJSON(w, r, 8<<10, &input) != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_request", "invalid Signals delivery attempt payload")
		return
	}
	updated, err := s.signals.RecordDeliveryAttempt(r.Context(), r.PathValue("deliveryID"), input.Succeeded, input.Retryable, input.ErrorCode)
	if err != nil {
		writeSignalsError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

func (s *Server) exportSignalsCSV(w http.ResponseWriter, r *http.Request, _ guard.Authentication) {
	if !s.requireSignals(w, r) {
		return
	}
	query, ok := signalAlertQuery(w, r)
	if !ok {
		return
	}
	content, err := s.signals.ExportCSV(r.Context(), query)
	if err != nil {
		writeSignalsError(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="stewardmesh-signals.csv"`)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(content)
}

func (s *Server) requireSignals(w http.ResponseWriter, r *http.Request) bool {
	if s.signals == nil {
		writeError(w, r, http.StatusServiceUnavailable, "signals_unavailable", "Signals are unavailable")
		return false
	}
	return true
}
func signalAlertQuery(w http.ResponseWriter, r *http.Request) (signals.AlertQuery, bool) {
	limit := 100
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil {
			writeError(w, r, http.StatusBadRequest, "validation_failed", "Signals alert limit is invalid")
			return signals.AlertQuery{}, false
		}
		limit = value
	}
	return signals.AlertQuery{Status: signals.AlertStatus(strings.TrimSpace(r.URL.Query().Get("status"))), Severity: signals.Severity(strings.TrimSpace(r.URL.Query().Get("severity"))), Condition: signals.Condition(strings.TrimSpace(r.URL.Query().Get("condition"))), Limit: limit}, true
}
func writeSignalsError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, signals.ErrInvalidInput):
		writeError(w, r, http.StatusBadRequest, "validation_failed", "Signals input is invalid")
	case errors.Is(err, signals.ErrNotFound):
		writeError(w, r, http.StatusNotFound, "not_found", "Signals record was not found")
	case errors.Is(err, signals.ErrConflict):
		writeError(w, r, http.StatusConflict, "conflict", "Signals state changed or conflicts with an existing record")
	default:
		writeError(w, r, http.StatusInternalServerError, "signals_error", "Signals operation could not be completed")
	}
}
