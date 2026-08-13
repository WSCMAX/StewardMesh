// Package grouperfixture provides an explicitly development-only, in-memory
// Grouper SCIM fixture. It is built only by the optional Compose integration
// profile and never participates in the default StewardMesh runtime.
// Requirement: REQ-DIRECTORY-EXPANSION-005. Feature: integrations.protocols.
package grouperfixture

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"
)

const maximumBodyBytes = 32 << 10

type Group struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	DisplayName string            `json:"displayName"`
	Description string            `json:"description,omitempty"`
	Active      bool              `json:"active"`
	Metadata    map[string]string `json:"metadata,omitempty"`
}

type Membership struct {
	ID          string            `json:"id"`
	GroupID     string            `json:"groupId"`
	MemberID    string            `json:"memberId"`
	MemberKind  string            `json:"memberKind"`
	DisplayName string            `json:"displayName"`
	Active      bool              `json:"active"`
	Metadata    map[string]string `json:"metadata,omitempty"`
}

type Server struct {
	mu          sync.RWMutex
	token       string
	groups      map[string]Group
	memberships map[string]Membership
}

func New(token string) (*Server, error) {
	if len(token) < 16 || len(token) > 512 {
		return nil, errors.New("fixture bearer token must contain 16 to 512 bytes")
	}
	return &Server{token: token, groups: map[string]Group{}, memberships: map[string]Membership{}}, nil
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.Handle("GET /grouper-ws/scim/v2/Groups", s.authorized(http.HandlerFunc(s.listGroups)))
	mux.Handle("POST /fixture/groups", s.authorized(http.HandlerFunc(s.createGroup)))
	mux.Handle("DELETE /fixture/groups/{id}", s.authorized(http.HandlerFunc(s.deleteGroup)))
	mux.Handle("POST /fixture/memberships", s.authorized(http.HandlerFunc(s.createMembership)))
	mux.Handle("DELETE /fixture/memberships/{id}", s.authorized(http.HandlerFunc(s.deleteMembership)))
	return mux
}

func (s *Server) authorized(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		provided := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if len(provided) != len(s.token) || subtle.ConstantTimeCompare([]byte(provided), []byte(s.token)) != 1 {
			w.Header().Set("WWW-Authenticate", "Bearer")
			writeError(w, http.StatusUnauthorized, "authorization is required")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) createGroup(w http.ResponseWriter, r *http.Request) {
	var group Group
	if err := decode(r, &group); err != nil {
		writeError(w, http.StatusBadRequest, "group input is invalid")
		return
	}
	group.ID, group.Name, group.DisplayName, group.Description = strings.TrimSpace(group.ID), strings.TrimSpace(group.Name), strings.TrimSpace(group.DisplayName), strings.TrimSpace(group.Description)
	if group.DisplayName == "" {
		group.DisplayName = group.Name
	}
	if !validID(group.ID) || !validText(group.Name, 512, false) || !validText(group.DisplayName, 200, false) ||
		!validText(group.Description, 2000, true) || !validMetadata(group.Metadata) {
		writeError(w, http.StatusBadRequest, "group input is invalid")
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.groups[group.ID]; exists {
		writeError(w, http.StatusConflict, "group already exists")
		return
	}
	s.groups[group.ID] = cloneGroup(group)
	writeJSON(w, http.StatusCreated, group)
}

func (s *Server) deleteGroup(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.PathValue("id"))
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.groups[id]; !exists {
		writeError(w, http.StatusNotFound, "group was not found")
		return
	}
	delete(s.groups, id)
	for membershipID, membership := range s.memberships {
		if membership.GroupID == id || membership.MemberKind == "group" && membership.MemberID == id {
			delete(s.memberships, membershipID)
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) createMembership(w http.ResponseWriter, r *http.Request) {
	var membership Membership
	if err := decode(r, &membership); err != nil {
		writeError(w, http.StatusBadRequest, "membership input is invalid")
		return
	}
	membership.ID, membership.GroupID, membership.MemberID = strings.TrimSpace(membership.ID), strings.TrimSpace(membership.GroupID), strings.TrimSpace(membership.MemberID)
	membership.MemberKind, membership.DisplayName = strings.ToLower(strings.TrimSpace(membership.MemberKind)), strings.TrimSpace(membership.DisplayName)
	if membership.DisplayName == "" {
		membership.DisplayName = membership.MemberID
	}
	if !validID(membership.ID) || !validID(membership.GroupID) || !validID(membership.MemberID) ||
		(membership.MemberKind != "subject" && membership.MemberKind != "group") || !validText(membership.DisplayName, 200, false) || !validMetadata(membership.Metadata) {
		writeError(w, http.StatusBadRequest, "membership input is invalid")
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.memberships[membership.ID]; exists {
		writeError(w, http.StatusConflict, "membership already exists")
		return
	}
	if _, exists := s.groups[membership.GroupID]; !exists {
		writeError(w, http.StatusBadRequest, "parent group does not exist")
		return
	}
	if membership.MemberKind == "group" {
		if _, exists := s.groups[membership.MemberID]; !exists || membership.MemberID == membership.GroupID {
			writeError(w, http.StatusBadRequest, "nested group does not exist or is recursive")
			return
		}
	}
	s.memberships[membership.ID] = cloneMembership(membership)
	writeJSON(w, http.StatusCreated, membership)
}

func (s *Server) deleteMembership(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.PathValue("id"))
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.memberships[id]; !exists {
		writeError(w, http.StatusNotFound, "membership was not found")
		return
	}
	delete(s.memberships, id)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) listGroups(w http.ResponseWriter, r *http.Request) {
	start, count := 1, 100
	var err error
	if value := r.URL.Query().Get("startIndex"); value != "" {
		start, err = strconv.Atoi(value)
	}
	if err == nil {
		if value := r.URL.Query().Get("count"); value != "" {
			count, err = strconv.Atoi(value)
		}
	}
	if err != nil || start < 1 || count < 1 || count > 250 || len(r.URL.Query()) > 2 {
		writeError(w, http.StatusBadRequest, "pagination is invalid")
		return
	}
	s.mu.RLock()
	groups := make([]Group, 0, len(s.groups))
	for _, group := range s.groups {
		groups = append(groups, cloneGroup(group))
	}
	memberships := make([]Membership, 0, len(s.memberships))
	for _, membership := range s.memberships {
		memberships = append(memberships, cloneMembership(membership))
	}
	s.mu.RUnlock()
	sort.Slice(groups, func(i, j int) bool { return groups[i].ID < groups[j].ID })
	sort.Slice(memberships, func(i, j int) bool { return memberships[i].ID < memberships[j].ID })
	from := start - 1
	if from > len(groups) {
		from = len(groups)
	}
	to := min(from+count, len(groups))
	type scimMember struct {
		Value     string            `json:"value"`
		Reference string            `json:"$ref"`
		Display   string            `json:"display"`
		Type      string            `json:"type"`
		Active    bool              `json:"active"`
		Metadata  map[string]string `json:"stewardMetadata,omitempty"`
	}
	type scimGroup struct {
		ID          string            `json:"id"`
		Name        string            `json:"name"`
		DisplayName string            `json:"displayName"`
		Description string            `json:"description,omitempty"`
		Active      bool              `json:"active"`
		Metadata    map[string]string `json:"stewardMetadata,omitempty"`
		Members     []scimMember      `json:"members"`
	}
	resources := make([]scimGroup, 0, to-from)
	for _, group := range groups[from:to] {
		item := scimGroup{ID: group.ID, Name: group.Name, DisplayName: group.DisplayName, Description: group.Description,
			Active: group.Active, Metadata: group.Metadata, Members: []scimMember{}}
		for _, membership := range memberships {
			if membership.GroupID != group.ID {
				continue
			}
			referenceKind := "Users"
			if membership.MemberKind == "group" {
				referenceKind = "Groups"
			}
			item.Members = append(item.Members, scimMember{Value: membership.MemberID, Display: membership.DisplayName,
				Type: membership.MemberKind, Reference: "/grouper-ws/scim/v2/" + referenceKind + "/" + membership.MemberID,
				Active: membership.Active, Metadata: membership.Metadata})
		}
		resources = append(resources, item)
	}
	writeJSON(w, http.StatusOK, map[string]any{"schemas": []string{"urn:ietf:params:scim:api:messages:2.0:ListResponse"},
		"totalResults": len(groups), "startIndex": start, "itemsPerPage": len(resources), "Resources": resources})
}

func decode(r *http.Request, target any) error {
	limited := &io.LimitedReader{R: r.Body, N: maximumBodyBytes + 1}
	decoder := json.NewDecoder(limited)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return errors.New("multiple JSON values")
	}
	if limited.N == 0 {
		return errors.New("body exceeds limit")
	}
	return nil
}

func validID(value string) bool { return validText(value, 255, false) }
func validText(value string, limit int, optional bool) bool {
	if (!optional && value == "") || !utf8.ValidString(value) || utf8.RuneCountInString(value) > limit {
		return false
	}
	for _, character := range value {
		if unicode.IsControl(character) {
			return false
		}
	}
	return true
}
func validMetadata(metadata map[string]string) bool {
	if len(metadata) > 16 {
		return false
	}
	for key, value := range metadata {
		if !validText(key, 64, false) || !validText(value, 500, true) {
			return false
		}
	}
	return true
}
func cloneGroup(group Group) Group { group.Metadata = cloneMap(group.Metadata); return group }
func cloneMembership(membership Membership) Membership {
	membership.Metadata = cloneMap(membership.Metadata)
	return membership
}
func cloneMap(values map[string]string) map[string]string {
	if len(values) == 0 {
		return nil
	}
	result := make(map[string]string, len(values))
	for key, value := range values {
		result[key] = value
	}
	return result
}
func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}
