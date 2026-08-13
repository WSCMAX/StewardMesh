package directoryexpansion

// Requirements: REQ-DIRECTORY-EXPANSION-002, REQ-DIRECTORY-EXPANSION-003.
// Features: integrations.protocols, identity.directory.

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"
	"unicode/utf8"
)

var (
	sourceSystemIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)
	providerNamePattern   = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)
)

// Registry is the runtime allowlist for server-configured connectors. It does
// not manufacture generic HTTP adapters: each provider slice must translate
// and sanitize its own API contract before registration.
type Registry struct {
	mu         sync.RWMutex
	connectors map[string]Connector
}

type registeredConnector struct {
	Connector
	system SourceSystem
}

func (c registeredConnector) SourceSystem() SourceSystem { return c.system }

func NewRegistry(connectors ...Connector) (*Registry, error) {
	registry := &Registry{connectors: make(map[string]Connector, len(connectors))}
	for _, connector := range connectors {
		if err := registry.Register(connector); err != nil {
			return nil, err
		}
	}
	return registry, nil
}

func (r *Registry) Register(connector Connector) error {
	if r == nil || connector == nil {
		return fmt.Errorf("%w: connector is required", ErrInvalidInput)
	}
	system := connector.SourceSystem()
	system.ID = strings.TrimSpace(system.ID)
	system.Provider = Provider(strings.ToLower(strings.TrimSpace(string(system.Provider))))
	system.ConfigRevision = strings.TrimSpace(system.ConfigRevision)
	if !sourceSystemIDPattern.MatchString(system.ID) || !providerNamePattern.MatchString(string(system.Provider)) ||
		system.ConfigRevision == "" || !utf8.ValidString(system.ConfigRevision) || utf8.RuneCountInString(system.ConfigRevision) > 128 {
		return fmt.Errorf("%w: connector source system identity is invalid", ErrInvalidInput)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.connectors) >= MaximumSources {
		return fmt.Errorf("%w: connector registry exceeds the source limit", ErrInvalidInput)
	}
	if _, exists := r.connectors[system.ID]; exists {
		return fmt.Errorf("%w: source system is already registered", ErrConflict)
	}
	// Preserve the exact normalized identity that was validated. Provider
	// adapters remain responsible only for page translation and cannot drift
	// source metadata after registration.
	r.connectors[system.ID] = registeredConnector{Connector: connector, system: system}
	return nil
}

func (r *Registry) Connector(sourceSystemID string) (Connector, bool) {
	if r == nil {
		return nil, false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	connector, ok := r.connectors[sourceSystemID]
	return connector, ok
}

// SourceSystems returns the safe, credential-free identities available to an
// authorized import operator. Connector implementation details and endpoints
// remain server-side configuration.
func (r *Registry) SourceSystems() []SourceSystem {
	if r == nil {
		return []SourceSystem{}
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	systems := make([]SourceSystem, 0, len(r.connectors))
	for _, connector := range r.connectors {
		systems = append(systems, connector.SourceSystem())
	}
	sort.Slice(systems, func(i, j int) bool { return systems[i].ID < systems[j].ID })
	return systems
}
