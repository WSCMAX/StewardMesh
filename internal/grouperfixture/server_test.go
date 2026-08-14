package grouperfixture

// Requirement: REQ-DIRECTORY-EXPANSION-005. Feature: integrations.protocols.

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestFixtureCreatesNestedGroupsMembershipsAndRemovesThem(t *testing.T) {
	server, err := New("development-token")
	if err != nil {
		t.Fatal(err)
	}
	request := func(method, path, body string, expected int) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer development-token")
		req.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		server.Handler().ServeHTTP(response, req)
		if response.Code != expected {
			t.Fatalf("%s %s returned %d: %s", method, path, response.Code, response.Body.String())
		}
		return response
	}
	request(http.MethodPost, "/fixture/groups", `{"id":"parent","name":"app:parent","displayName":"Parent","active":true}`, http.StatusCreated)
	request(http.MethodPost, "/fixture/groups", `{"id":"child","name":"app:child","displayName":"Child","active":true}`, http.StatusCreated)
	request(http.MethodPost, "/fixture/memberships", `{"id":"nested","groupId":"parent","memberId":"child","memberKind":"group","displayName":"Child","active":true}`, http.StatusCreated)
	response := request(http.MethodGet, "/grouper-ws/scim/v2/Groups?startIndex=2&count=1", "", http.StatusOK)
	var page struct {
		TotalResults int `json:"totalResults"`
		Resources    []struct {
			Members []struct{ Value, Type string } `json:"members"`
		} `json:"Resources"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &page); err != nil || page.TotalResults != 2 || len(page.Resources) != 1 ||
		len(page.Resources[0].Members) != 1 || page.Resources[0].Members[0].Type != "group" {
		t.Fatalf("unexpected fixture page %#v err=%v", page, err)
	}
	request(http.MethodDelete, "/fixture/memberships/nested", "", http.StatusNoContent)
	request(http.MethodDelete, "/fixture/groups/child", "", http.StatusNoContent)
}

func TestFixtureRequiresAuthorizationRejectsDuplicatesAndBoundsInput(t *testing.T) {
	server, _ := New("development-token")
	handler := server.Handler()
	unauthorized := httptest.NewRecorder()
	handler.ServeHTTP(unauthorized, httptest.NewRequest(http.MethodGet, "/grouper-ws/scim/v2/Groups", nil))
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("fixture allowed unauthenticated read: %d", unauthorized.Code)
	}
	create := func(body string) int {
		req := httptest.NewRequest(http.MethodPost, "/fixture/groups", strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer development-token")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, req)
		return response.Code
	}
	body := `{"id":"same","name":"app:same","displayName":"Same","active":true}`
	if create(body) != http.StatusCreated || create(body) != http.StatusConflict {
		t.Fatal("fixture did not reject duplicate group")
	}
	largeRequest := httptest.NewRequest(http.MethodPost, "/fixture/groups", bytes.NewReader(bytes.Repeat([]byte("x"), maximumBodyBytes+1)))
	largeRequest.Header.Set("Authorization", "Bearer development-token")
	largeResponse := httptest.NewRecorder()
	handler.ServeHTTP(largeResponse, largeRequest)
	if largeResponse.Code != http.StatusBadRequest {
		t.Fatalf("fixture accepted oversized body: %d", largeResponse.Code)
	}
}
