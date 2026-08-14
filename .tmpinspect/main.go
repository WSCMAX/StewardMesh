package main

import (
  "bytes"
  "context"
  "encoding/json"
  "fmt"
  "io"
  "net/http"
  "net/http/httptest"
  "os"
  "strings"
  "archive/zip"

  "github.com/maxlemke/stewardmesh/internal/application"
  "github.com/maxlemke/stewardmesh/internal/config"
  "github.com/maxlemke/stewardmesh/internal/exchange"
)

func main() {
  cfg := config.DefaultConfig()
  cfg.OrganizationID = "signals-exchange-source"
  cfg.OrganizationName = "signals-exchange-source"
  cfg.ExchangeSourceSystemID = "signals-source-system"
  cfg.AllowedOrigin = "http://localhost:3000"
  app, err := application.New(context.Background(), cfg, application.Options{})
  if err != nil { panic(err) }
  defer app.Close()
  cookie, csrf := bootstrapApplicationAdministrator(app, cfg.AllowedOrigin)
  do := func(method, path, body string) *httptest.ResponseRecorder {
    req := httptest.NewRequest(method, path, strings.NewReader(body))
    req.Header.Set("Content-Type", "application/json")
    req.Header.Set("Origin", cfg.AllowedOrigin)
    req.Header.Set("X-CSRF-Token", csrf)
    req.AddCookie(cookie)
    res := httptest.NewRecorder(); app.Handler().ServeHTTP(res, req); return res
  }
  res := do("POST","/api/v1/signals/rules", `{"id":"portable-rule","name":"Portable renewals","condition":"renewal","severity":"warning","enabled":false,"thresholdDays":[365,90,30]}`)
  fmt.Println("create", res.Code, res.Body.String())
  exp := do("POST","/api/v1/exchange/export", `{"selection":[{"type":"signals.rule","id":"portable-rule"}],"fileMode":"metadata"}`)
  fmt.Println("export code", exp.Code, "content-type", exp.Header().Get("Content-Type"), "len", len(exp.Body.Bytes()))
  // dump archive entries
  zr, err := zip.NewReader(bytes.NewReader(exp.Body.Bytes()), int64(len(exp.Body.Bytes())))
  if err != nil { panic(err) }
  for _, f := range zr.File {
    rc, err := f.Open(); if err != nil { panic(err) }
    b, err := io.ReadAll(rc); rc.Close(); if err != nil { panic(err) }
    fmt.Println("ENTRY:", f.Name, "bytes", len(b))
    fmt.Println(string(b))
    var v any
    if err := json.Unmarshal(b, &v); err == nil { fmt.Printf("JSON type=%T value=%#v\n", v, v) }
  }
  decoded, _, err := exchange.DecodeArchiveForDebug(exp.Body.Bytes())
  fmt.Println("decodeArchive err", err)
  if err == nil { fmt.Printf("manifest=%#v\n", decoded.Manifest) }
  os.RemoveAll(".tmpinspect")
}
func bootstrapApplicationAdministrator(app *application.Application, origin string) (*http.Cookie, string) {
  // Start with session bootstrap creation by direct handler path using a temp config built to mimic tests.
  req := httptest.NewRequest("POST", "/api/v1/bootstrap", strings.NewReader(`{"username":"admin","email":"admin@example.com","displayName":"Admin","password":"test-password-123"}`))
  req.Header.Set("Content-Type", "application/json")
  req.Header.Set("Origin", origin)
  res := httptest.NewRecorder(); app.Handler().ServeHTTP(res, req)
  if res.Code != http.StatusCreated && res.Code != http.StatusOK { panic("bootstrap failed: "+res.Body.String()) }
  // parse set-cookie
  cookies := res.Result().Cookies()
  if len(cookies) == 0 { panic("missing session cookie") }
  csrf := ""
  for _, c := range cookies {
    if c.Name == "_csrf" { csrf = c.Value }
  }
  return cookies[0], csrf
}
