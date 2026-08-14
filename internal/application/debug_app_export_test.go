package application

import (
    "archive/zip"
    "bytes"
    "context"
    "encoding/json"
    "io"
    "net/http"
    "net/http/httptest"
    "strings"
    "testing"

    "github.com/maxlemke/stewardmesh/internal/config"
    "github.com/maxlemke/stewardmesh/internal/exchange"
)

func TestDebugAppExchangeImport(t *testing.T) {
    newApplication := func(organizationID, sourceSystemID string) (*Application, config.Config, *http.Cookie, string) {
        cfg := memoryConfiguration(t)
        cfg.OrganizationID, cfg.OrganizationName, cfg.ExchangeSourceSystemID = organizationID, organizationID, sourceSystemID
        app, err := New(context.Background(), cfg, Options{})
        if err != nil { t.Fatal(err) }
        t.Cleanup(func() { _ = app.Close() })
        cookie, csrfToken := bootstrapApplicationAdministrator(t, app, cfg.AllowedOrigin)
        return app, cfg, cookie, csrfToken
    }
    doWrite := func(app *Application, cfg config.Config, cookie *http.Cookie, csrf, method, path, body string) *httptest.ResponseRecorder {
        request := httptest.NewRequest(method, path, strings.NewReader(body))
        request.Header.Set("Content-Type", "application/json")
        request.Header.Set("Origin", cfg.AllowedOrigin)
        request.Header.Set("X-CSRF-Token", csrf)
        request.AddCookie(cookie)
        response := httptest.NewRecorder()
        app.Handler().ServeHTTP(response, request)
        return response
    }
    source, sourceConfig, sourceCookie, sourceCSRF := newApplication("signals-debug-source", "signals-debug-system")
    created := doWrite(source, sourceConfig, sourceCookie, sourceCSRF, http.MethodPost, "/api/v1/signals/rules", `{
        "id":"portable-rule","name":"Portable renewals","condition":"renewal","severity":"warning","enabled":false,"thresholdDays":[365,90,30]
    }`)
    if created.Code != http.StatusCreated {
        t.Fatalf("create source: %d %s", created.Code, created.Body.String())
    }
    exported := doWrite(source, sourceConfig, sourceCookie, sourceCSRF, http.MethodPost, "/api/v1/exchange/export", `{
        "selection":[{"type":"signals.rule","id":"portable-rule"}],"fileMode":"metadata"
    }`)
    if exported.Code != http.StatusOK {
        t.Fatalf("export status: %d %s", exported.Code, exported.Body.String())
    }
    t.Logf("export content type=%s len=%d", exported.Header().Get("Content-Type"), len(exported.Body.Bytes()))
    t.Logf("export body=%s", string(exported.Body.Bytes()[:min(400, len(exported.Body.Bytes()))]))
    manifest := exchangeManifestBytesForDebug(t, exported.Body.Bytes())
    t.Logf("manifest=%s", string(manifest[:min(2000, len(manifest))]))
    target, targetConfig, targetCookie, targetCSRF := newApplication("signals-debug-target", "signals-debug-system")
    req := httptest.NewRequest(http.MethodPost, "/api/v1/exchange/import", bytes.NewReader(exported.Body.Bytes()))
    req.Header.Set("Content-Type", exchange.MediaType)
    req.Header.Set("Origin", targetConfig.AllowedOrigin)
    req.Header.Set("X-CSRF-Token", targetCSRF)
    req.AddCookie(targetCookie)
    resp := httptest.NewRecorder()
    target.Handler().ServeHTTP(resp, req)
    t.Logf("import code=%d body=%s", resp.Code, resp.Body.String())
    var body map[string]any
    if err := json.Unmarshal(resp.Body.Bytes(), &body); err == nil { t.Logf("json=%#v", body) }
    if resp.Code != http.StatusCreated { t.FailNow() }
}

func min(a, b int) int { if a < b { return a }; return b }

func exchangeManifestBytesForDebug(t *testing.T, contents []byte) []byte {
    t.Helper()
    reader, err := zip.NewReader(bytes.NewReader(contents), int64(len(contents)))
    if err != nil { t.Fatal(err) }
    for _, f := range reader.File { if f.Name == "manifest.json" { r, err := f.Open(); if err != nil { t.Fatal(err) }; defer r.Close(); b, err := io.ReadAll(r); if err != nil { t.Fatal(err) }; return b } }
    t.Fatal("manifest missing")
    return nil
}
