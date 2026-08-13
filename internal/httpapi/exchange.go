package httpapi

// Requirement: REQ-EXCHANGE-001. Feature: migration.packages. GitHub: #9.

import (
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strconv"
	"strings"

	"github.com/maxlemke/stewardmesh/internal/exchange"
	"github.com/maxlemke/stewardmesh/internal/guard"
)

const maximumExchangeExportRequestBytes = 128 << 10

func (s *Server) listExchangeRecords(w http.ResponseWriter, r *http.Request, _ guard.Authentication) {
	if s.exchange == nil {
		writeError(w, r, http.StatusServiceUnavailable, "exchange_unavailable", "Exchange packages are unavailable")
		return
	}
	records, err := s.exchange.ListRecords(r.Context())
	if err != nil {
		writeExchangeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"items": records,
		"limits": map[string]any{
			"maximumArchiveBytes": exchange.MaximumArchiveBytes,
			"maximumSelections":   exchange.MaximumSelections,
		},
	})
}

func (s *Server) listExchangePackages(w http.ResponseWriter, r *http.Request, _ guard.Authentication) {
	if s.exchange == nil {
		writeError(w, r, http.StatusServiceUnavailable, "exchange_unavailable", "Exchange packages are unavailable")
		return
	}
	limit := 25
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil {
			writeError(w, r, http.StatusBadRequest, "validation_failed", "Exchange history limit is invalid")
			return
		}
		limit = parsed
	}
	packages, err := s.exchange.ListPackages(r.Context(), limit)
	if err != nil {
		writeExchangeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": packages})
}

func (s *Server) exportExchangePackage(w http.ResponseWriter, r *http.Request, authentication guard.Authentication) {
	if s.exchange == nil {
		writeError(w, r, http.StatusServiceUnavailable, "exchange_unavailable", "Exchange packages are unavailable")
		return
	}
	var input exchange.ExportRequest
	if err := decodeJSON(w, r, maximumExchangeExportRequestBytes, &input); err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_request", "the Exchange export selection is invalid")
		return
	}
	artifact, err := s.exchange.Export(r.Context(), authentication.Principal.Subject, input)
	if err != nil {
		writeExchangeError(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", exchange.MediaType)
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="stewardmesh-%s.openinventory"`, artifact.PackageID))
	w.Header().Set("Content-Length", strconv.Itoa(len(artifact.Bytes)))
	w.Header().Set("X-Content-SHA256", artifact.SHA256)
	w.Header().Set("X-Exchange-Package-ID", artifact.PackageID)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(artifact.Bytes)
}

func (s *Server) importExchangePackage(w http.ResponseWriter, r *http.Request, authentication guard.Authentication) {
	if s.exchange == nil {
		writeError(w, r, http.StatusServiceUnavailable, "exchange_unavailable", "Exchange packages are unavailable")
		return
	}
	if strings.TrimSpace(r.Header.Get("Content-Encoding")) != "" {
		writeError(w, r, http.StatusUnsupportedMediaType, "content_encoding_invalid", "Exchange packages must not use HTTP content encoding")
		return
	}
	contentType, parameters, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || contentType != exchange.MediaType || len(parameters) != 0 {
		writeError(w, r, http.StatusUnsupportedMediaType, "content_type_invalid", "Exchange imports require an .openinventory package")
		return
	}
	if r.ContentLength > exchange.MaximumArchiveBytes {
		writeError(w, r, http.StatusRequestEntityTooLarge, "package_too_large", "the Exchange package exceeds the 32 MiB limit")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, exchange.MaximumArchiveBytes)
	contents, err := io.ReadAll(r.Body)
	if err != nil {
		var maximumError *http.MaxBytesError
		if errors.As(err, &maximumError) {
			writeError(w, r, http.StatusRequestEntityTooLarge, "package_too_large", "the Exchange package exceeds the 32 MiB limit")
			return
		}
		writeError(w, r, http.StatusBadRequest, "invalid_request", "the Exchange package could not be read")
		return
	}
	result, err := s.exchange.Import(r.Context(), authentication.Principal.Subject, contents)
	if err != nil {
		writeExchangeError(w, r, err)
		return
	}
	status := http.StatusCreated
	if result.Replay {
		status = http.StatusOK
	}
	w.Header().Set("X-Idempotent-Replay", strconv.FormatBool(result.Replay))
	writeJSON(w, status, result)
}

func writeExchangeError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, exchange.ErrTooLarge):
		writeError(w, r, http.StatusRequestEntityTooLarge, "package_too_large", "the Exchange package exceeds a configured limit")
	case errors.Is(err, exchange.ErrIntegrity):
		writeError(w, r, http.StatusUnprocessableEntity, "integrity_failed", "the Exchange package is corrupt or failed checksum verification")
	case errors.Is(err, exchange.ErrDependencyMissing):
		writeError(w, r, http.StatusUnprocessableEntity, "dependency_missing", "an Exchange package dependency is unavailable")
	case errors.Is(err, exchange.ErrInvalidInput):
		writeError(w, r, http.StatusBadRequest, "validation_failed", "the Exchange package or selection is invalid")
	case errors.Is(err, exchange.ErrNotFound):
		writeError(w, r, http.StatusNotFound, "not_found", "a selected Exchange record was not found")
	case errors.Is(err, exchange.ErrConflict):
		writeError(w, r, http.StatusConflict, "package_conflict", "the Exchange package conflicts with durable history or current records")
	default:
		writeError(w, r, http.StatusInternalServerError, "exchange_error", "the Exchange operation could not be completed")
	}
}
