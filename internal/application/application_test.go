package application

// Requirements: REQ-FOUNDATION-001, REQ-ATLAS-001, REQ-ATLAS-CATALOG-001, REQ-ATLAS-CODES-001, REQ-DIRECTORY-EXPANSION-003, REQ-DIRECTORY-EXPANSION-004, REQ-DIRECTORY-EXPANSION-005, REQ-DIRECTORY-EXPANSION-006, REQ-DIRECTORY-EXPANSION-007, REQ-DIRECTORY-EXPANSION-008, REQ-PATTERNS-001, REQ-STORAGE-001, REQ-LEDGER-001, REQ-HORIZON-001, REQ-EXCHANGE-001, REQ-PLATFORM-VALKEY-001, SEC-GUARD-001.
// Features: inventory.assets, inventory.identifiers, inventory.catalog, procurement.finance, lifecycle.planning, templates.schemas, migration.packages.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/maxlemke/stewardmesh/internal/bridge"
	"github.com/maxlemke/stewardmesh/internal/catalog"
	"github.com/maxlemke/stewardmesh/internal/config"
	"github.com/maxlemke/stewardmesh/internal/directoryexpansion"
	"github.com/maxlemke/stewardmesh/internal/exchange"
	"github.com/maxlemke/stewardmesh/internal/foundation"
	"github.com/maxlemke/stewardmesh/internal/grouperfixture"
	"github.com/maxlemke/stewardmesh/internal/guard"
	"github.com/maxlemke/stewardmesh/internal/horizon"
)

func TestNewBuildsReusableMemoryApplication(t *testing.T) {
	cfg := memoryConfiguration(t)
	app, err := New(context.Background(), cfg, Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := app.Close(); err != nil {
			t.Fatal(err)
		}
	})
	if app.Organization().ID != cfg.OrganizationID || app.Organization().Name != cfg.OrganizationName {
		t.Fatalf("unexpected organization %#v", app.Organization())
	}

	request := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	response := httptest.NewRecorder()
	app.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("expected reusable handler health response, got %d: %s", response.Code, response.Body.String())
	}
	var body map[string]string
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["organizationId"] != cfg.OrganizationID {
		t.Fatalf("unexpected health response %#v", body)
	}
	templatesRequest := httptest.NewRequest(http.MethodGet, "/api/v1/templates", nil)
	templatesResponse := httptest.NewRecorder()
	app.Handler().ServeHTTP(templatesResponse, templatesRequest)
	if templatesResponse.Code != http.StatusUnauthorized {
		t.Fatalf("expected wired Patterns service to require authentication, got %d: %s", templatesResponse.Code, templatesResponse.Body.String())
	}
}

func TestNewRegistersImplementedExchangeProviders(t *testing.T) {
	cfg := memoryConfiguration(t)
	app, err := New(context.Background(), cfg, Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := app.Close(); err != nil {
			t.Fatal(err)
		}
	})
	if app.Catalog() == nil {
		t.Fatal("application did not retain the Atlas Catalog service")
	}
	if app.Horizon() == nil {
		t.Fatal("application did not retain the Horizon service")
	}
	cookie, _ := bootstrapApplicationAdministrator(t, app, cfg.AllowedOrigin)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/exchange/records", nil)
	request.AddCookie(cookie)
	response := httptest.NewRecorder()
	app.Handler().ServeHTTP(response, request)
	var body struct {
		PortableRecordTypes      []string `json:"portableRecordTypes"`
		RegisteredRecordTypes    []string `json:"registeredRecordTypes"`
		ProviderRegistryComplete bool     `json:"providerRegistryComplete"`
	}
	if response.Code != http.StatusOK {
		t.Fatalf("list application Exchange records: %d %s", response.Code, response.Body.String())
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode application Exchange registry: %v", err)
	}
	want := exchange.PortableRecordTypes()
	if !body.ProviderRegistryComplete || !reflect.DeepEqual(body.PortableRecordTypes, want) || !reflect.DeepEqual(body.RegisteredRecordTypes, want) {
		t.Fatalf("application Exchange registry is not the exact portable boundary: %#v", body)
	}
}

func TestApplicationSignalsExchangeRoundTripLocksOrdinaryRuleMutation(t *testing.T) {
	newApplication := func(organizationID, sourceSystemID string) (*Application, config.Config, *http.Cookie, string) {
		t.Helper()
		cfg := memoryConfiguration(t)
		cfg.OrganizationID, cfg.OrganizationName, cfg.ExchangeSourceSystemID = organizationID, organizationID, sourceSystemID
		app, err := New(context.Background(), cfg, Options{})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = app.Close() })
		cookie, csrf := bootstrapApplicationAdministrator(t, app, cfg.AllowedOrigin)
		return app, cfg, cookie, csrf
	}
	doWrite := func(app *Application, cfg config.Config, cookie *http.Cookie, csrf, method, path, body string) *httptest.ResponseRecorder {
		t.Helper()
		request := httptest.NewRequest(method, path, strings.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Origin", cfg.AllowedOrigin)
		request.Header.Set("X-CSRF-Token", csrf)
		request.AddCookie(cookie)
		response := httptest.NewRecorder()
		app.Handler().ServeHTTP(response, request)
		return response
	}

	source, sourceConfig, sourceCookie, sourceCSRF := newApplication("signals-exchange-source", "signals-source-system")
	created := doWrite(source, sourceConfig, sourceCookie, sourceCSRF, http.MethodPost, "/api/v1/signals/rules", `{
		"id":"portable-rule","name":"Portable renewals","condition":"renewal","severity":"warning","enabled":false,"thresholdDays":[365,90,30]
	}`)
	if created.Code != http.StatusCreated {
		t.Fatalf("create source Signals rule: %d %s", created.Code, created.Body.String())
	}
	exported := doWrite(source, sourceConfig, sourceCookie, sourceCSRF, http.MethodPost, "/api/v1/exchange/export", `{
		"selection":[{"type":"signals.rule","id":"portable-rule"}],"fileMode":"metadata"
	}`)
	if exported.Code != http.StatusOK || exported.Header().Get("Content-Type") != exchange.MediaType {
		t.Fatalf("export Signals package: %d %s", exported.Code, exported.Body.String())
	}
	target, targetConfig, targetCookie, targetCSRF := newApplication("signals-exchange-target", "signals-target-system")
	request := httptest.NewRequest(http.MethodPost, "/api/v1/exchange/import", bytes.NewReader(exported.Body.Bytes()))
	request.Header.Set("Content-Type", exchange.MediaType)
	request.Header.Set("Origin", targetConfig.AllowedOrigin)
	request.Header.Set("X-CSRF-Token", targetCSRF)
	request.AddCookie(targetCookie)
	imported := httptest.NewRecorder()
	target.Handler().ServeHTTP(imported, request)
	if imported.Code != http.StatusCreated || !strings.Contains(imported.Body.String(), `"type":"signals.rule"`) || !strings.Contains(imported.Body.String(), `"writeLocked":true`) {
		t.Fatalf("import Signals package: %d %s", imported.Code, imported.Body.String())
	}
	locked := doWrite(target, targetConfig, targetCookie, targetCSRF, http.MethodPut, "/api/v1/signals/rules/portable-rule", `{
		"name":"Local overwrite","condition":"renewal","severity":"critical","enabled":true,"thresholdDays":[30],"revision":1
	}`)
	if locked.Code != http.StatusLocked || !strings.Contains(locked.Body.String(), `"code":"ownership_locked"`) {
		t.Fatalf("expected imported Signals service ownership fence: %d %s", locked.Code, locked.Body.String())
	}
}

func TestApplicationBridgeExchangeRoundTripLocksOrdinaryRevoke(t *testing.T) {
	newApplication := func(organizationID, sourceSystemID string) (*Application, config.Config, *http.Cookie, string) {
		t.Helper()
		cfg := memoryConfiguration(t)
		cfg.OrganizationID, cfg.OrganizationName, cfg.ExchangeSourceSystemID = organizationID, organizationID, sourceSystemID
		app, err := New(context.Background(), cfg, Options{})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = app.Close() })
		cookie, csrfToken := bootstrapApplicationAdministrator(t, app, cfg.AllowedOrigin)
		return app, cfg, cookie, csrfToken
	}
	doWrite := func(app *Application, cfg config.Config, cookie *http.Cookie, csrfToken, method, path, contentType string, body io.Reader) *httptest.ResponseRecorder {
		t.Helper()
		request := httptest.NewRequest(method, path, body)
		if contentType != "" {
			request.Header.Set("Content-Type", contentType)
		}
		request.Header.Set("Origin", cfg.AllowedOrigin)
		request.Header.Set("X-CSRF-Token", csrfToken)
		request.AddCookie(cookie)
		response := httptest.NewRecorder()
		app.Handler().ServeHTTP(response, request)
		return response
	}

	source, sourceConfig, sourceCookie, sourceCSRF := newApplication("bridge-exchange-source", "bridge-source-system")
	createdResponse := doWrite(source, sourceConfig, sourceCookie, sourceCSRF, http.MethodPost, "/api/v1/bridge/clients", "application/json", strings.NewReader(`{
		"name":"Portable public client","redirectUris":["http://127.0.0.1:8181/callback","https://client.example.test/callback"],
		"allowedScopes":["assets:read","mcp:resources"]
	}`))
	var created bridge.Client
	if createdResponse.Code != http.StatusCreated || json.Unmarshal(createdResponse.Body.Bytes(), &created) != nil || created.ID == "" {
		t.Fatalf("create source Bridge client: %d %s", createdResponse.Code, createdResponse.Body.String())
	}
	exported := doWrite(source, sourceConfig, sourceCookie, sourceCSRF, http.MethodPost, "/api/v1/exchange/export", "application/json", strings.NewReader(`{
		"selection":[{"type":"bridge.oauth-client","id":"`+created.ID+`"}],"fileMode":"metadata"
	}`))
	if exported.Code != http.StatusOK || exported.Header().Get("Content-Type") != exchange.MediaType {
		t.Fatalf("export Bridge client: %d %s", exported.Code, exported.Body.String())
	}

	target, targetConfig, targetCookie, targetCSRF := newApplication("bridge-exchange-target", "bridge-target-system")
	imported := doWrite(target, targetConfig, targetCookie, targetCSRF, http.MethodPost, "/api/v1/exchange/import", exchange.MediaType, bytes.NewReader(exported.Body.Bytes()))
	if imported.Code != http.StatusCreated || !strings.Contains(imported.Body.String(), `"type":"bridge.oauth-client"`) ||
		!strings.Contains(imported.Body.String(), `"writeLocked":true`) {
		t.Fatalf("import Bridge client: %d %s", imported.Code, imported.Body.String())
	}
	locked := doWrite(target, targetConfig, targetCookie, targetCSRF, http.MethodDelete, "/api/v1/bridge/clients/"+created.ID, "", nil)
	if locked.Code != http.StatusLocked || !strings.Contains(locked.Body.String(), `"code":"ownership_locked"`) {
		t.Fatalf("expected imported Bridge client ownership fence: %d %s", locked.Code, locked.Body.String())
	}
}

func TestApplicationThreadsExchangeImportLocksOrdinaryWrites(t *testing.T) {
	newApplication := func(organizationID, sourceSystemID string) (*Application, config.Config, *http.Cookie, string) {
		t.Helper()
		cfg := memoryConfiguration(t)
		cfg.OrganizationID = organizationID
		cfg.OrganizationName = organizationID
		cfg.ExchangeSourceSystemID = sourceSystemID
		app, err := New(context.Background(), cfg, Options{})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if err := app.Close(); err != nil {
				t.Fatal(err)
			}
		})
		cookie, csrfToken := bootstrapApplicationAdministrator(t, app, cfg.AllowedOrigin)
		return app, cfg, cookie, csrfToken
	}
	doWrite := func(app *Application, cfg config.Config, cookie *http.Cookie, csrfToken, method, path, body string) *httptest.ResponseRecorder {
		t.Helper()
		request := httptest.NewRequest(method, path, strings.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Origin", cfg.AllowedOrigin)
		request.Header.Set("X-CSRF-Token", csrfToken)
		request.AddCookie(cookie)
		response := httptest.NewRecorder()
		app.Handler().ServeHTTP(response, request)
		return response
	}

	source, sourceConfig, sourceCookie, sourceCSRF := newApplication("threads-exchange-source", "threads-source-system")
	createResponse := doWrite(source, sourceConfig, sourceCookie, sourceCSRF, http.MethodPost, "/api/v1/tags", `{
		"id":"portable-thread","name":"Portable thread","inheritByDefault":true
	}`)
	if createResponse.Code != http.StatusCreated {
		t.Fatalf("create source Threads tag: %d %s", createResponse.Code, createResponse.Body.String())
	}
	exportResponse := doWrite(source, sourceConfig, sourceCookie, sourceCSRF, http.MethodPost, "/api/v1/exchange/export", `{
		"selection":[{"type":"threads.tag","id":"portable-thread"}],"fileMode":"metadata"
	}`)
	if exportResponse.Code != http.StatusOK || exportResponse.Header().Get("Content-Type") != exchange.MediaType {
		t.Fatalf("export Threads package: %d %s", exportResponse.Code, exportResponse.Body.String())
	}

	target, targetConfig, targetCookie, targetCSRF := newApplication("threads-exchange-target", "threads-target-system")
	importRequest := httptest.NewRequest(http.MethodPost, "/api/v1/exchange/import", bytes.NewReader(exportResponse.Body.Bytes()))
	importRequest.Header.Set("Content-Type", exchange.MediaType)
	importRequest.Header.Set("Origin", targetConfig.AllowedOrigin)
	importRequest.Header.Set("X-CSRF-Token", targetCSRF)
	importRequest.AddCookie(targetCookie)
	importResponse := httptest.NewRecorder()
	target.Handler().ServeHTTP(importResponse, importRequest)
	if importResponse.Code != http.StatusCreated || !strings.Contains(importResponse.Body.String(), `"type":"threads.tag"`) ||
		!strings.Contains(importResponse.Body.String(), `"writeLocked":true`) {
		t.Fatalf("import Threads package: %d %s", importResponse.Code, importResponse.Body.String())
	}
	updateResponse := doWrite(target, targetConfig, targetCookie, targetCSRF, http.MethodPut, "/api/v1/tags/portable-thread", `{
		"name":"Local overwrite","inheritByDefault":false,"revision":1
	}`)
	if updateResponse.Code != http.StatusLocked || !strings.Contains(updateResponse.Body.String(), `"code":"ownership_locked"`) {
		t.Fatalf("expected imported Threads service ownership fence: %d %s", updateResponse.Code, updateResponse.Body.String())
	}
}

func TestApplicationPatternsExchangeRoundTripLocksOrdinaryVersions(t *testing.T) {
	newApplication := func(organizationID, sourceSystemID string) (*Application, config.Config, *http.Cookie, string) {
		t.Helper()
		cfg := memoryConfiguration(t)
		cfg.OrganizationID, cfg.OrganizationName, cfg.ExchangeSourceSystemID = organizationID, organizationID, sourceSystemID
		app, err := New(context.Background(), cfg, Options{})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = app.Close() })
		cookie, csrfToken := bootstrapApplicationAdministrator(t, app, cfg.AllowedOrigin)
		return app, cfg, cookie, csrfToken
	}
	doWrite := func(app *Application, cfg config.Config, cookie *http.Cookie, csrfToken, method, path, body string) *httptest.ResponseRecorder {
		t.Helper()
		request := httptest.NewRequest(method, path, strings.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Origin", cfg.AllowedOrigin)
		request.Header.Set("X-CSRF-Token", csrfToken)
		request.AddCookie(cookie)
		response := httptest.NewRecorder()
		app.Handler().ServeHTTP(response, request)
		return response
	}

	source, sourceConfig, sourceCookie, sourceCSRF := newApplication("patterns-exchange-source", "patterns-source-system")
	created := doWrite(source, sourceConfig, sourceCookie, sourceCSRF, http.MethodPost, "/api/v1/templates", `{
		"id":"portable-template","recordType":"example.record","name":"Portable template","description":"First",
		"fields":[{"key":"name","label":"Name","type":"text","required":true}]
	}`)
	if created.Code != http.StatusCreated {
		t.Fatalf("create source Patterns template: %d %s", created.Code, created.Body.String())
	}
	version := doWrite(source, sourceConfig, sourceCookie, sourceCSRF, http.MethodPost, "/api/v1/templates/portable-template/versions", `{
		"description":"Second","fields":[{"key":"name","label":"Name","type":"text","required":true},{"key":"state","label":"State","type":"enum","options":["new","ready"]}]
	}`)
	if version.Code != http.StatusCreated {
		t.Fatalf("create source Patterns version: %d %s", version.Code, version.Body.String())
	}
	exported := doWrite(source, sourceConfig, sourceCookie, sourceCSRF, http.MethodPost, "/api/v1/exchange/export", `{
		"selection":[{"type":"patterns.template","id":"portable-template"}],"fileMode":"metadata"
	}`)
	if exported.Code != http.StatusOK || exported.Header().Get("Content-Type") != exchange.MediaType {
		t.Fatalf("export Patterns package: %d %s", exported.Code, exported.Body.String())
	}

	target, targetConfig, targetCookie, targetCSRF := newApplication("patterns-exchange-target", "patterns-target-system")
	importRequest := httptest.NewRequest(http.MethodPost, "/api/v1/exchange/import", bytes.NewReader(exported.Body.Bytes()))
	importRequest.Header.Set("Content-Type", exchange.MediaType)
	importRequest.Header.Set("Origin", targetConfig.AllowedOrigin)
	importRequest.Header.Set("X-CSRF-Token", targetCSRF)
	importRequest.AddCookie(targetCookie)
	imported := httptest.NewRecorder()
	target.Handler().ServeHTTP(imported, importRequest)
	if imported.Code != http.StatusCreated || !strings.Contains(imported.Body.String(), `"type":"patterns.template"`) || !strings.Contains(imported.Body.String(), `"writeLocked":true`) {
		t.Fatalf("import Patterns package: %d %s", imported.Code, imported.Body.String())
	}
	history, err := target.Patterns().ExchangeTemplate(context.Background(), "portable-template")
	if err != nil || len(history.Versions) != 2 || history.Versions[1].Description != "Second" {
		t.Fatalf("Patterns application import was incomplete: %#v err=%v", history, err)
	}
	locked := doWrite(target, targetConfig, targetCookie, targetCSRF, http.MethodPost, "/api/v1/templates/portable-template/versions", `{
		"description":"Local overwrite","fields":[{"key":"name","label":"Name","type":"text","required":true}]
	}`)
	if locked.Code != http.StatusLocked || !strings.Contains(locked.Body.String(), `"code":"ownership_locked"`) {
		t.Fatalf("expected imported Patterns service ownership fence: %d %s", locked.Code, locked.Body.String())
	}
}

func TestApplicationLedgerExchangeRoundTripReturnsLockedForOrdinaryWrite(t *testing.T) {
	newApplication := func(organizationID, sourceSystemID string) (*Application, config.Config, *http.Cookie, string) {
		t.Helper()
		cfg := memoryConfiguration(t)
		cfg.OrganizationID, cfg.OrganizationName, cfg.ExchangeSourceSystemID = organizationID, organizationID, sourceSystemID
		app, err := New(context.Background(), cfg, Options{})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = app.Close() })
		cookie, csrfToken := bootstrapApplicationAdministrator(t, app, cfg.AllowedOrigin)
		return app, cfg, cookie, csrfToken
	}
	doWrite := func(app *Application, cfg config.Config, cookie *http.Cookie, csrfToken, method, path, body string) *httptest.ResponseRecorder {
		t.Helper()
		request := httptest.NewRequest(method, path, strings.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Origin", cfg.AllowedOrigin)
		request.Header.Set("X-CSRF-Token", csrfToken)
		request.AddCookie(cookie)
		response := httptest.NewRecorder()
		app.Handler().ServeHTTP(response, request)
		return response
	}

	source, sourceConfig, sourceCookie, sourceCSRF := newApplication("ledger-exchange-source", "ledger-source-system")
	created := doWrite(source, sourceConfig, sourceCookie, sourceCSRF, http.MethodPost, "/api/v1/ledger/vendors", `{
		"id":"portable-vendor","name":"Portable vendor","externalId":"vendor/42","status":"active"
	}`)
	if created.Code != http.StatusCreated {
		t.Fatalf("create source Ledger vendor: %d %s", created.Code, created.Body.String())
	}
	exported := doWrite(source, sourceConfig, sourceCookie, sourceCSRF, http.MethodPost, "/api/v1/exchange/export", `{
		"selection":[{"type":"ledger.vendor","id":"portable-vendor"}],"fileMode":"metadata"
	}`)
	if exported.Code != http.StatusOK || exported.Header().Get("Content-Type") != exchange.MediaType {
		t.Fatalf("export Ledger package: %d %s", exported.Code, exported.Body.String())
	}

	target, targetConfig, targetCookie, targetCSRF := newApplication("ledger-exchange-target", "ledger-target-system")
	importRequest := httptest.NewRequest(http.MethodPost, "/api/v1/exchange/import", bytes.NewReader(exported.Body.Bytes()))
	importRequest.Header.Set("Content-Type", exchange.MediaType)
	importRequest.Header.Set("Origin", targetConfig.AllowedOrigin)
	importRequest.Header.Set("X-CSRF-Token", targetCSRF)
	importRequest.AddCookie(targetCookie)
	imported := httptest.NewRecorder()
	target.Handler().ServeHTTP(imported, importRequest)
	if imported.Code != http.StatusCreated || !strings.Contains(imported.Body.String(), `"type":"ledger.vendor"`) || !strings.Contains(imported.Body.String(), `"writeLocked":true`) {
		t.Fatalf("import Ledger package: %d %s", imported.Code, imported.Body.String())
	}
	locked := doWrite(target, targetConfig, targetCookie, targetCSRF, http.MethodPost, "/api/v1/ledger/vendors", `{
		"id":"portable-vendor","name":"Local overwrite","status":"active"
	}`)
	if locked.Code != http.StatusLocked || !strings.Contains(locked.Body.String(), `"code":"ownership_locked"`) {
		t.Fatalf("expected imported Ledger HTTP ownership fence: %d %s", locked.Code, locked.Body.String())
	}
}

func TestApplicationCatalogExchangeImportLocksLocalServiceWrites(t *testing.T) {
	newApplication := func(organizationID, sourceSystemID string) (*Application, config.Config, *http.Cookie, string) {
		t.Helper()
		cfg := memoryConfiguration(t)
		cfg.OrganizationID = organizationID
		cfg.OrganizationName = organizationID
		cfg.ExchangeSourceSystemID = sourceSystemID
		app, err := New(context.Background(), cfg, Options{})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if err := app.Close(); err != nil {
				t.Fatal(err)
			}
		})
		cookie, csrfToken := bootstrapApplicationAdministrator(t, app, cfg.AllowedOrigin)
		return app, cfg, cookie, csrfToken
	}
	createModel := func(app *Application, cfg config.Config, cookie *http.Cookie, csrfToken string) {
		t.Helper()
		request := httptest.NewRequest(http.MethodPost, "/api/v1/asset-models", bytes.NewBufferString(`{
			"id":"portable-model","manufacturer":"Example","name":"Portable model","kind":"server"
		}`))
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Origin", cfg.AllowedOrigin)
		request.Header.Set("X-CSRF-Token", csrfToken)
		request.AddCookie(cookie)
		response := httptest.NewRecorder()
		app.Handler().ServeHTTP(response, request)
		if response.Code != http.StatusCreated {
			t.Fatalf("create Atlas model: %d %s", response.Code, response.Body.String())
		}
	}
	authenticatedContext := func(app *Application, cfg config.Config, cookie *http.Cookie, correlationID string) context.Context {
		t.Helper()
		authentication, err := app.Guard().AuthenticateSession(context.Background(), cookie.Value)
		if err != nil {
			t.Fatal(err)
		}
		return foundation.WithScope(context.Background(), foundation.Scope{
			OrganizationID: cfg.OrganizationID, ActorID: authentication.Principal.Subject, CorrelationID: correlationID,
		})
	}

	source, sourceConfig, sourceCookie, sourceCSRF := newApplication("catalog-exchange-source", "catalog-source-system")
	createModel(source, sourceConfig, sourceCookie, sourceCSRF)
	configuration, err := source.Catalog().CreateConfiguration(
		authenticatedContext(source, sourceConfig, sourceCookie, "catalog-source-create"),
		catalog.CreateConfigurationInput{ID: "portable-configuration", ModelID: "portable-model", Name: "Portable", Status: catalog.StatusActive},
	)
	if err != nil {
		t.Fatal(err)
	}
	exportBody, err := json.Marshal(exchange.ExportRequest{
		Selection: []exchange.Reference{{Type: "atlas.catalog-configuration", ID: configuration.ID}},
		FileMode:  exchange.FileModeMetadata,
	})
	if err != nil {
		t.Fatal(err)
	}
	exportRequest := httptest.NewRequest(http.MethodPost, "/api/v1/exchange/export", bytes.NewReader(exportBody))
	exportRequest.Header.Set("Content-Type", "application/json")
	exportRequest.Header.Set("Origin", sourceConfig.AllowedOrigin)
	exportRequest.Header.Set("X-CSRF-Token", sourceCSRF)
	exportRequest.AddCookie(sourceCookie)
	exportResponse := httptest.NewRecorder()
	source.Handler().ServeHTTP(exportResponse, exportRequest)
	if exportResponse.Code != http.StatusOK || exportResponse.Header().Get("Content-Type") != exchange.MediaType {
		t.Fatalf("export Catalog package: %d %s", exportResponse.Code, exportResponse.Body.String())
	}

	target, targetConfig, targetCookie, targetCSRF := newApplication("catalog-exchange-target", "catalog-target-system")
	createModel(target, targetConfig, targetCookie, targetCSRF)
	importRequest := httptest.NewRequest(http.MethodPost, "/api/v1/exchange/import", bytes.NewReader(exportResponse.Body.Bytes()))
	importRequest.Header.Set("Content-Type", exchange.MediaType)
	importRequest.Header.Set("Origin", targetConfig.AllowedOrigin)
	importRequest.Header.Set("X-CSRF-Token", targetCSRF)
	importRequest.AddCookie(targetCookie)
	importResponse := httptest.NewRecorder()
	target.Handler().ServeHTTP(importResponse, importRequest)
	if importResponse.Code != http.StatusCreated || !strings.Contains(importResponse.Body.String(), `"writeLocked":true`) {
		t.Fatalf("import Catalog package: %d %s", importResponse.Code, importResponse.Body.String())
	}
	_, err = target.Catalog().CreateConfiguration(
		authenticatedContext(target, targetConfig, targetCookie, "catalog-target-write"),
		catalog.CreateConfigurationInput{ID: configuration.ID, ModelID: "portable-model", Name: "Local overwrite", Status: catalog.StatusActive},
	)
	if !errors.Is(err, guard.ErrResourceWriteLocked) {
		t.Fatalf("expected imported Catalog ownership lock at service boundary, got %v", err)
	}
}

func TestApplicationHorizonExchangeRoundTripLocksOrdinaryUpdates(t *testing.T) {
	newApplication := func(organizationID, sourceSystemID string) (*Application, config.Config, *http.Cookie, string) {
		t.Helper()
		cfg := memoryConfiguration(t)
		cfg.OrganizationID = organizationID
		cfg.OrganizationName = organizationID
		cfg.ExchangeSourceSystemID = sourceSystemID
		app, err := New(context.Background(), cfg, Options{})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if err := app.Close(); err != nil {
				t.Fatal(err)
			}
		})
		cookie, csrfToken := bootstrapApplicationAdministrator(t, app, cfg.AllowedOrigin)
		return app, cfg, cookie, csrfToken
	}
	authenticatedContext := func(app *Application, cfg config.Config, cookie *http.Cookie, correlationID string) context.Context {
		t.Helper()
		authentication, err := app.Guard().AuthenticateSession(context.Background(), cookie.Value)
		if err != nil {
			t.Fatal(err)
		}
		return foundation.WithScope(context.Background(), foundation.Scope{
			OrganizationID: cfg.OrganizationID, ActorID: authentication.Principal.Subject, CorrelationID: correlationID,
		})
	}
	createAsset := func(app *Application, cfg config.Config, cookie *http.Cookie, csrfToken string) {
		t.Helper()
		request := httptest.NewRequest(http.MethodPost, "/api/v1/assets", bytes.NewBufferString(`{
			"id":"portable-asset","name":"Portable asset","kind":"server","status":"active"
		}`))
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Origin", cfg.AllowedOrigin)
		request.Header.Set("X-CSRF-Token", csrfToken)
		request.AddCookie(cookie)
		response := httptest.NewRecorder()
		app.Handler().ServeHTTP(response, request)
		if response.Code != http.StatusCreated {
			t.Fatalf("create Horizon Atlas dependency: %d %s", response.Code, response.Body.String())
		}
	}

	source, sourceConfig, sourceCookie, sourceCSRF := newApplication("horizon-exchange-source", "horizon-source-system")
	createAsset(source, sourceConfig, sourceCookie, sourceCSRF)
	replacement := time.Date(2030, time.June, 30, 0, 0, 0, 0, time.UTC)
	plan, err := source.Horizon().CreatePlan(
		authenticatedContext(source, sourceConfig, sourceCookie, "horizon-source-create"),
		horizon.CreatePlanInput{
			ID: "portable-plan", AssetID: "portable-asset", Scenario: "baseline", ExpectedUsefulLifeMonths: 60,
			ReplacementDate: &replacement, LifecycleStage: "approved", ReplacementCostMinor: 450_000,
			Currency: "USD", EffectiveFrom: time.Date(2027, time.January, 1, 0, 0, 0, 0, time.UTC),
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	exportBody, err := json.Marshal(exchange.ExportRequest{
		Selection: []exchange.Reference{{Type: "horizon.plan", ID: plan.ID}}, FileMode: exchange.FileModeMetadata,
	})
	if err != nil {
		t.Fatal(err)
	}
	exportRequest := httptest.NewRequest(http.MethodPost, "/api/v1/exchange/export", bytes.NewReader(exportBody))
	exportRequest.Header.Set("Content-Type", "application/json")
	exportRequest.Header.Set("Origin", sourceConfig.AllowedOrigin)
	exportRequest.Header.Set("X-CSRF-Token", sourceCSRF)
	exportRequest.AddCookie(sourceCookie)
	exportResponse := httptest.NewRecorder()
	source.Handler().ServeHTTP(exportResponse, exportRequest)
	if exportResponse.Code != http.StatusOK || exportResponse.Header().Get("Content-Type") != exchange.MediaType {
		t.Fatalf("export Horizon package: %d %s", exportResponse.Code, exportResponse.Body.String())
	}

	target, targetConfig, targetCookie, targetCSRF := newApplication("horizon-exchange-target", "horizon-target-system")
	createAsset(target, targetConfig, targetCookie, targetCSRF)
	importRequest := httptest.NewRequest(http.MethodPost, "/api/v1/exchange/import", bytes.NewReader(exportResponse.Body.Bytes()))
	importRequest.Header.Set("Content-Type", exchange.MediaType)
	importRequest.Header.Set("Origin", targetConfig.AllowedOrigin)
	importRequest.Header.Set("X-CSRF-Token", targetCSRF)
	importRequest.AddCookie(targetCookie)
	importResponse := httptest.NewRecorder()
	target.Handler().ServeHTTP(importResponse, importRequest)
	if importResponse.Code != http.StatusCreated || !strings.Contains(importResponse.Body.String(), `"writeLocked":true`) ||
		!strings.Contains(importResponse.Body.String(), `"type":"horizon.plan"`) {
		t.Fatalf("import Horizon package: %d %s", importResponse.Code, importResponse.Body.String())
	}
	imported, err := target.Horizon().GetPlan(context.Background(), plan.ID)
	if err != nil || imported.Revision != plan.Revision || imported.AssetID != plan.AssetID ||
		imported.LifecycleStage != plan.LifecycleStage || imported.ReplacementCostMinor != plan.ReplacementCostMinor ||
		imported.ReplacementDate == nil || !imported.ReplacementDate.Equal(*plan.ReplacementDate) {
		t.Fatalf("Horizon application import was not lossless: %#v err=%v", imported, err)
	}
	_, err = target.Horizon().UpdatePlan(
		authenticatedContext(target, targetConfig, targetCookie, "horizon-target-write"),
		horizon.UpdatePlanInput{
			ID: imported.ID, AssetID: imported.AssetID, Scenario: imported.Scenario,
			ExpectedUsefulLifeMonths: imported.ExpectedUsefulLifeMonths, ReplacementDate: imported.ReplacementDate,
			LifecycleStage: "retired", ReplacementCostMinor: imported.ReplacementCostMinor, Currency: imported.Currency,
			EffectiveFrom: imported.EffectiveFrom, Revision: imported.Revision,
		},
	)
	if !errors.Is(err, guard.ErrResourceWriteLocked) {
		t.Fatalf("expected imported Horizon ownership fence at service boundary, got %v", err)
	}
}

func TestNewRegistersOptionalMicrosoftEntraConnectorWithoutStartupNetworkWrites(t *testing.T) {
	cfg := memoryConfiguration(t)
	cfg.EntraSourceSystemID = "entra-primary"
	cfg.EntraTenantID = "11111111-1111-4111-8111-111111111111"
	cfg.EntraClientID = "22222222-2222-4222-8222-222222222222"
	cfg.EntraClientSecret = "0123456789abcdef"
	app, err := New(context.Background(), cfg, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if err := app.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestNewRegistersOptionalSailPointConnectorWithoutStartupNetworkWrites(t *testing.T) {
	cfg := memoryConfiguration(t)
	cfg.SailPointSourceSystemID = "sailpoint-primary"
	cfg.SailPointBaseURL = "https://example.api.identitynow.com"
	cfg.SailPointClientID = "0123456789abcdef"
	cfg.SailPointClientSecret = "abcdef0123456789"
	app, err := New(context.Background(), cfg, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if err := app.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestNewRegistersOptionalPeopleSoftConnectorWithoutStartupNetworkWrites(t *testing.T) {
	cfg := memoryConfiguration(t)
	cfg.PeopleSoftSourceSystemID = "peoplesoft-primary"
	cfg.PeopleSoftBaseURL = "https://peoplesoft.example.test/PSIGW/RESTListeningConnector/CAMPUS/ExecuteQuery.v1"
	cfg.PeopleSoftUsername = "integration-reader"
	cfg.PeopleSoftPassword = "secret-manager-password"
	cfg.PeopleSoftQueryOwner = "public"
	cfg.PeopleSoftOrganizationQuery = "SM_ORGANIZATIONS"
	cfg.PeopleSoftLocationQuery = "SM_LOCATIONS"
	cfg.PeopleSoftBuildingQuery = "SM_BUILDINGS"
	cfg.PeopleSoftDepartmentQuery = "SM_DEPARTMENTS"
	cfg.PeopleSoftFieldMappingsJSON = `{"organization":{"setId":{"selector":"A.SETID","alias":"SETID"},"id":{"selector":"A.ORG_ID","alias":"ORG_ID"},"name":{"selector":"A.DESCR","alias":"DESCR"},"status":{"selector":"A.EFF_STATUS","alias":"EFF_STATUS"}},"location":{"setId":{"selector":"A.SETID","alias":"SETID"},"id":{"selector":"A.LOCATION","alias":"LOCATION"},"name":{"selector":"A.DESCR","alias":"DESCR"},"status":{"selector":"A.EFF_STATUS","alias":"EFF_STATUS"},"organizationId":{"selector":"A.ORG_ID","alias":"ORG_ID"}},"building":{"setId":{"selector":"A.SETID","alias":"SETID"},"id":{"selector":"A.BUILDING","alias":"BUILDING"},"name":{"selector":"A.DESCR","alias":"DESCR"},"status":{"selector":"A.EFF_STATUS","alias":"EFF_STATUS"},"locationId":{"selector":"A.LOCATION","alias":"LOCATION"}},"department":{"setId":{"selector":"A.SETID","alias":"SETID"},"id":{"selector":"A.DEPTID","alias":"DEPTID"},"name":{"selector":"A.DESCR","alias":"DESCR"},"status":{"selector":"A.EFF_STATUS","alias":"EFF_STATUS"},"organizationId":{"selector":"A.ORG_ID","alias":"ORG_ID"},"locationId":{"selector":"A.LOCATION","alias":"LOCATION"}}}`
	app, err := New(context.Background(), cfg, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if err := app.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestNewSeedsClearlyLabeledSyntheticDemoLocationsPeopleMappingsAndRelationships(t *testing.T) {
	cfg := memoryConfiguration(t)
	cfg.OrganizationID = "demo-application"
	cfg.OrganizationName = "[Synthetic Demo] Application"
	cfg.ExchangeSourceSystemID = cfg.OrganizationID
	cfg.SeedSynthetic = true
	app, err := New(context.Background(), cfg, Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := app.Close(); err != nil {
			t.Fatal(err)
		}
	})

	cookie, csrfToken := bootstrapApplicationAdministrator(t, app, cfg.AllowedOrigin)
	get := func(path string, target any) {
		t.Helper()
		request := httptest.NewRequest(http.MethodGet, path, nil)
		request.AddCookie(cookie)
		response := httptest.NewRecorder()
		app.Handler().ServeHTTP(response, request)
		if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), target) != nil {
			t.Fatalf("GET %s: %d %s", path, response.Code, response.Body.String())
		}
	}
	_ = csrfToken // Bootstrap also proves the synthetic seed did not create an authentication account.

	var sources struct {
		Items []directoryexpansion.SourceSystem `json:"items"`
	}
	get("/api/v1/directory-import-sources", &sources)
	if len(sources.Items) != 1 || sources.Items[0].ID != directoryexpansion.SyntheticSourceSystemID || sources.Items[0].Provider != directoryexpansion.SyntheticProvider {
		t.Fatalf("unexpected synthetic source catalog %#v", sources.Items)
	}
	var sites struct {
		Items []struct {
			ID, Name string
		} `json:"items"`
	}
	get("/api/v1/sites", &sites)
	if len(sites.Items) != 1 || !strings.HasPrefix(sites.Items[0].Name, "[Synthetic Demo]") {
		t.Fatalf("unexpected synthetic site response %#v", sites.Items)
	}
	var identities struct {
		Items []struct {
			DisplayName, Provider, ProviderSubject string
		} `json:"items"`
	}
	get("/api/v1/identities?limit=100", &identities)
	if len(identities.Items) != 3 {
		t.Fatalf("unexpected synthetic identities %#v", identities.Items)
	}
	for _, identity := range identities.Items {
		if !strings.HasPrefix(identity.DisplayName, "[Synthetic Demo]") || !strings.HasPrefix(identity.Provider, "directory.") || identity.ProviderSubject == "" {
			t.Fatalf("synthetic identity is not labeled and provider-isolated: %#v", identity)
		}
	}
	var graph directoryexpansion.Graph
	get("/api/v1/graph", &graph)
	if len(graph.Nodes) != 12 || len(graph.Edges) != 12 {
		t.Fatalf("unexpected complete synthetic relationship graph %#v", graph)
	}
	for _, expectedKind := range []directoryexpansion.NodeKind{
		directoryexpansion.NodeOrganization, directoryexpansion.NodeSite, directoryexpansion.NodeBuilding,
		directoryexpansion.NodeRoom, directoryexpansion.NodeDepartment, directoryexpansion.NodePerson,
		directoryexpansion.NodeShared, directoryexpansion.NodeGroup, directoryexpansion.NodeSubject,
	} {
		found := false
		for _, node := range graph.Nodes {
			if node.Kind == expectedKind {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("synthetic relationship graph omitted %s: %#v", expectedKind, graph.Nodes)
		}
	}
}

func TestNewWiresConfiguredGrouperThroughPreviewApplyAndGraph(t *testing.T) {
	const token = "application-fixture-token"
	fixture, err := grouperfixture.New(token)
	if err != nil {
		t.Fatal(err)
	}
	create := httptest.NewRequest(http.MethodPost, "/fixture/groups", bytes.NewBufferString(`{"id":"researchers","name":"app:researchers","displayName":"Researchers","active":true}`))
	create.Header.Set("Authorization", "Bearer "+token)
	create.Header.Set("Content-Type", "application/json")
	created := httptest.NewRecorder()
	fixture.Handler().ServeHTTP(created, create)
	if created.Code != http.StatusCreated {
		t.Fatalf("create fixture group: %d %s", created.Code, created.Body.String())
	}
	provider := httptest.NewServer(fixture.Handler())
	defer provider.Close()

	cfg := memoryConfiguration(t)
	cfg.GrouperURL = provider.URL + "/grouper-ws/scim/v2"
	cfg.GrouperSourceSystemID = "application-grouper"
	cfg.GrouperBearerToken = token
	cfg.GrouperConfigRevision = "test-v1"
	cfg.GrouperAllowPrivateNetwork = true
	app, err := New(context.Background(), cfg, Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := app.Close(); err != nil {
			t.Fatal(err)
		}
	})

	bootstrapPayload := bytes.NewBufferString(`{"username":"administrator","email":"administrator@example.test","displayName":"Administrator","password":"correct horse battery staple"}`)
	bootstrapRequest := httptest.NewRequest(http.MethodPost, "/api/v1/auth/bootstrap", bootstrapPayload)
	bootstrapRequest.Header.Set("Content-Type", "application/json")
	bootstrapRequest.Header.Set("Origin", cfg.AllowedOrigin)
	bootstrapResponse := httptest.NewRecorder()
	app.Handler().ServeHTTP(bootstrapResponse, bootstrapRequest)
	var credentials struct {
		CSRFToken string `json:"csrfToken"`
	}
	if bootstrapResponse.Code != http.StatusCreated || json.Unmarshal(bootstrapResponse.Body.Bytes(), &credentials) != nil ||
		credentials.CSRFToken == "" || len(bootstrapResponse.Result().Cookies()) != 1 {
		t.Fatalf("bootstrap application administrator: %d %s", bootstrapResponse.Code, bootstrapResponse.Body.String())
	}
	cookie := bootstrapResponse.Result().Cookies()[0]
	authenticated := func(method, path string, body *bytes.Buffer, idempotencyKey string) *http.Request {
		var requestBody io.Reader
		if body != nil {
			requestBody = body
		}
		request := httptest.NewRequest(method, path, requestBody)
		request.AddCookie(cookie)
		if method != http.MethodGet {
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set("Origin", cfg.AllowedOrigin)
			request.Header.Set("X-CSRF-Token", credentials.CSRFToken)
		}
		if idempotencyKey != "" {
			request.Header.Set("Idempotency-Key", idempotencyKey)
		}
		return request
	}

	previewResponse := httptest.NewRecorder()
	app.Handler().ServeHTTP(previewResponse, authenticated(http.MethodPost, "/api/v1/directory-imports/preview",
		bytes.NewBufferString(`{"sourceSystemId":"application-grouper"}`), "application-grouper-preview"))
	var preview directoryexpansion.OperationResult
	if previewResponse.Code != http.StatusCreated || json.Unmarshal(previewResponse.Body.Bytes(), &preview) != nil ||
		preview.Batch.Provider != directoryexpansion.GrouperProvider || preview.Batch.Counts.Created != 1 {
		t.Fatalf("preview configured Grouper: %d %s", previewResponse.Code, previewResponse.Body.String())
	}
	applyResponse := httptest.NewRecorder()
	app.Handler().ServeHTTP(applyResponse, authenticated(http.MethodPost, "/api/v1/directory-imports/"+preview.Batch.ID+"/apply", nil, "application-grouper-apply"))
	if applyResponse.Code != http.StatusOK {
		t.Fatalf("apply configured Grouper: %d %s", applyResponse.Code, applyResponse.Body.String())
	}
	graphResponse := httptest.NewRecorder()
	app.Handler().ServeHTTP(graphResponse, authenticated(http.MethodGet, "/api/v1/graph", nil, ""))
	var graph directoryexpansion.Graph
	if graphResponse.Code != http.StatusOK || json.Unmarshal(graphResponse.Body.Bytes(), &graph) != nil ||
		len(graph.Nodes) != 2 || len(graph.Edges) != 1 {
		t.Fatalf("read configured Grouper graph: %d %s", graphResponse.Code, graphResponse.Body.String())
	}
	foundGroup := false
	for _, node := range graph.Nodes {
		if node.Kind == directoryexpansion.NodeGroup && node.Label == "Researchers" {
			foundGroup = true
		}
	}
	if !foundGroup {
		t.Fatalf("configured Grouper node missing from typed graph: %#v", graph.Nodes)
	}
}

func TestNewSupportsMemoryCacheAndFailsClosedForUnavailableValkey(t *testing.T) {
	memory := memoryConfiguration(t)
	memory.CacheDriver = config.CacheDriverMemory
	app, err := New(context.Background(), memory, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if err := app.Close(); err != nil {
		t.Fatal(err)
	}

	valkey := memoryConfiguration(t)
	valkey.CacheDriver = config.CacheDriverValkey
	valkey.CacheURL = "redis://127.0.0.1:6379/0"
	valkey.CacheKeySecret = "0123456789abcdef0123456789abcdef"
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if app, err := New(canceled, valkey, Options{}); err == nil || app != nil {
		t.Fatalf("expected configured Valkey startup to fail closed, app=%T err=%v", app, err)
	}
}

func TestInitializeAttemptLimiterPreservesDisabledModeAndSupportsMemory(t *testing.T) {
	disabled := memoryConfiguration(t)
	limiter, closeCache, err := initializeAttemptLimiter(context.Background(), disabled)
	if err != nil {
		t.Fatal(err)
	}
	if limiter != nil {
		t.Fatal("expected disabled cache mode to preserve Guard's local limiter")
	}
	if err := closeCache(); err != nil {
		t.Fatal(err)
	}

	memory := disabled
	memory.CacheDriver = config.CacheDriverMemory
	limiter, closeCache, err = initializeAttemptLimiter(context.Background(), memory)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := closeCache(); err != nil {
			t.Fatal(err)
		}
	})
	if limiter == nil {
		t.Fatal("expected memory cache mode to create a cache-backed limiter")
	}
	allowed, err := limiter.Allow(context.Background(), "account|administrator", time.Now())
	if err != nil || !allowed {
		t.Fatalf("expected initialized memory limiter to allow, allowed=%t err=%v", allowed, err)
	}
}

func TestCloseIsIdempotentAndReleasesResourcesInReverseOrder(t *testing.T) {
	var order []string
	cacheError := errors.New("cache close")
	foundationError := errors.New("foundation close")
	app := &Application{
		closeCache: func() error {
			order = append(order, "cache")
			return cacheError
		},
		closeFoundation: func() error {
			order = append(order, "foundation")
			return foundationError
		},
	}
	for range 2 {
		err := app.Close()
		if !errors.Is(err, cacheError) || !errors.Is(err, foundationError) {
			t.Fatalf("expected joined close errors, got %v", err)
		}
	}
	if !reflect.DeepEqual(order, []string{"cache", "foundation"}) {
		t.Fatalf("unexpected close order %v", order)
	}
}

func TestNewRejectsMissingContextAndInvalidConfiguration(t *testing.T) {
	cfg := memoryConfiguration(t)
	if app, err := New(nil, cfg, Options{}); err == nil || app != nil {
		t.Fatalf("expected nil context to fail, app=%T err=%v", app, err)
	}
	cfg.BlobDir = ""
	if app, err := New(context.Background(), cfg, Options{}); err == nil || app != nil {
		t.Fatalf("expected invalid blob configuration to fail, app=%T err=%v", app, err)
	}
}

func TestNewBuildsPostgresApplicationWithExplicitMigrations(t *testing.T) {
	databaseURL := os.Getenv("STEWARDMESH_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("STEWARDMESH_TEST_DATABASE_URL is not configured")
	}
	cfg := memoryConfiguration(t)
	cfg.RepositoryDriver = config.RepositoryDriverPostgres
	cfg.DatabaseURL = databaseURL
	cfg.OrganizationID = fmt.Sprintf("application-construction-%d", time.Now().UnixNano())
	cfg.OrganizationName = "Application Construction"
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	app, err := New(ctx, cfg, Options{RunMigrations: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := app.Close(); err != nil {
			t.Fatal(err)
		}
	})
	if app.Organization().ID != cfg.OrganizationID {
		t.Fatalf("unexpected organization %#v", app.Organization())
	}
}

func memoryConfiguration(t *testing.T) config.Config {
	t.Helper()
	cfg := config.FromEnv()
	cfg.Addr = "127.0.0.1:8080"
	cfg.RepositoryDriver = config.RepositoryDriverMemory
	cfg.DatabaseURL = ""
	cfg.CacheDriver = config.CacheDriverNone
	cfg.CacheURL = ""
	cfg.CacheKeySecret = ""
	cfg.OIDCIssuerURL = ""
	cfg.OIDCClientID = ""
	cfg.OIDCClientSecret = ""
	cfg.OIDCRedirectURL = ""
	cfg.OIDCTransactionSecret = ""
	cfg.OIDCAdministratorClaim = ""
	cfg.OIDCAdministratorValues = nil
	cfg.EntraSourceSystemID = "entra"
	cfg.EntraTenantID = ""
	cfg.EntraClientID = ""
	cfg.EntraClientSecret = ""
	cfg.SailPointSourceSystemID = "sailpoint"
	cfg.SailPointBaseURL = ""
	cfg.SailPointClientID = ""
	cfg.SailPointClientSecret = ""
	cfg.BlobDir = t.TempDir()
	cfg.AllowedOrigin = "http://localhost:5173"
	cfg.SessionCookieSecure = false
	cfg.BootstrapToken = ""
	cfg.SessionTTL = time.Hour
	cfg.SeedSynthetic = false
	cfg.OrganizationID = "application-test"
	cfg.OrganizationName = "Application Test"
	cfg.GrouperURL = ""
	cfg.GrouperSourceSystemID = ""
	cfg.GrouperUsername = ""
	cfg.GrouperPassword = ""
	cfg.GrouperBearerToken = ""
	cfg.GrouperConfigRevision = ""
	cfg.GrouperPageSize = directoryexpansion.DefaultGrouperPageSize
	cfg.GrouperMaximumResponseBytes = directoryexpansion.DefaultGrouperResponseBytes
	cfg.GrouperTimeout = directoryexpansion.DefaultGrouperTimeout
	cfg.GrouperAllowPrivateNetwork = false
	cfg.ExchangeSourceSystemID = "application-test"
	cfg.PeopleSoftSourceSystemID = "peoplesoft"
	cfg.PeopleSoftBaseURL = ""
	cfg.PeopleSoftUsername = ""
	cfg.PeopleSoftPassword = ""
	cfg.PeopleSoftBearerToken = ""
	cfg.PeopleSoftQueryOwner = "public"
	cfg.PeopleSoftOrganizationQuery = ""
	cfg.PeopleSoftLocationQuery = ""
	cfg.PeopleSoftBuildingQuery = ""
	cfg.PeopleSoftDepartmentQuery = ""
	cfg.PeopleSoftFieldMappingsJSON = ""
	cfg.PeopleSoftMaximumRows = directoryexpansion.DefaultPeopleSoftMaximumRows
	cfg.PeopleSoftResponseBytes = directoryexpansion.DefaultPeopleSoftResponseBytes
	cfg.PeopleSoftTimeout = directoryexpansion.DefaultPeopleSoftTimeout
	cfg.PeopleSoftAllowPrivate = false
	return cfg
}

func bootstrapApplicationAdministrator(t *testing.T, app *Application, allowedOrigin string) (*http.Cookie, string) {
	t.Helper()
	payload := bytes.NewBufferString(`{"username":"administrator","email":"administrator@example.test","displayName":"Administrator","password":"correct horse battery staple"}`)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/auth/bootstrap", payload)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", allowedOrigin)
	response := httptest.NewRecorder()
	app.Handler().ServeHTTP(response, request)
	var credentials struct {
		CSRFToken string `json:"csrfToken"`
	}
	if response.Code != http.StatusCreated || json.Unmarshal(response.Body.Bytes(), &credentials) != nil || credentials.CSRFToken == "" || len(response.Result().Cookies()) != 1 {
		t.Fatalf("bootstrap application administrator: %d %s", response.Code, response.Body.String())
	}
	return response.Result().Cookies()[0], credentials.CSRFToken
}
