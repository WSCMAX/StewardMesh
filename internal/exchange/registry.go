package exchange

// Requirements: REQ-EXCHANGE-001, REQ-PATTERNS-001. Features: migration.packages, templates.schemas. GitHub: #9, #8.

import (
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/maxlemke/stewardmesh/internal/patterns"
)

// ExcludedRecordType documents a deliberate Exchange boundary. Exclusions are
// reserved for deployment-owned, security-sensitive, derived, operational, or
// self-referential records. A domain is never excluded merely because its
// provider has not been implemented yet.
type ExcludedRecordType struct {
	Type   string `json:"type"`
	Reason string `json:"reason"`
}

var portableRecordTypes = []string{
	"atlas.asset", "atlas.model", "atlas.identifier", "atlas.lifecycle-event",
	"atlas.catalog-configuration", "atlas.catalog-price", "atlas.catalog-upgrade-path",
	"people.site", "people.building", "people.room", "people.department", "people.identity", "people.assignment",
	"threads.tag", "threads.goal", "threads.tag-rule", "threads.goal-link",
	"labels.definition", "labels.assignment",
	"vault.blob",
	"ledger.vendor", "ledger.purchase-order", "ledger.contract", "ledger.commitment", "ledger.budget", "ledger.cost",
	"horizon.plan",
	"patterns.template",
	"stack.product", "stack.version", "stack.installation", "stack.license", "stack.assignment",
	"signals.rule", "signals.subscription",
	"reach.provider", "reach.template", "reach.subscriber-group",
	"directory.group", "directory.membership",
	"bridge.oauth-client",
}

var excludedRecordTypes = []ExcludedRecordType{
	{Type: "foundation.organization", Reason: "the target organization is a deployment-owned security boundary"},
	{Type: "atlas.label-template", Reason: "label layouts are immutable code-owned definitions with multiple active variants"},
	{Type: "guard.role", Reason: "authorization policy is destination-owned and importing it could widen privilege"},
	{Type: "guard.account", Reason: "accounts and authentication credentials are destination-owned security state"},
	{Type: "guard.policy-bundle", Reason: "authorization policy is destination-owned and importing it could widen privilege"},
	{Type: "guard.role-assignment", Reason: "authorization assignments are destination-owned and importing them could widen privilege"},
	{Type: "guard.resource-ownership", Reason: "Exchange creates destination ownership locks and cannot recursively import its control plane"},
	{Type: "signals.alert", Reason: "alerts are derived operational state and are regenerated from imported rules and domain records"},
	{Type: "reach.message", Reason: "queued and delivered messages are operational state whose import could replay delivery"},
	{Type: "directory.import-batch", Reason: "connector execution receipts are not portable without their deliberately excluded intake and attempt rows"},
	{Type: "exchange.package", Reason: "package receipts are destination-local and self-referential"},
	{Type: "bridge.oauth-grant", Reason: "OAuth grants are credential-bearing session state and cannot be reconstructed without excluded token hashes"},
}

// PortableRecordTypes returns the complete phase-one Exchange provider
// boundary in deterministic order.
func PortableRecordTypes() []string {
	result := append([]string(nil), portableRecordTypes...)
	sort.Strings(result)
	return result
}

// ExplicitlyExcludedRecordTypes returns the complete, reasoned phase-one
// Exchange exclusions in deterministic order.
func ExplicitlyExcludedRecordTypes() []ExcludedRecordType {
	result := append([]ExcludedRecordType(nil), excludedRecordTypes...)
	sort.Slice(result, func(i, j int) bool { return result[i].Type < result[j].Type })
	return result
}

// ValidateRecordTypeBoundary proves that every Patterns core record family is
// classified exactly once. Keeping this executable prevents a new phase-one
// domain from silently disappearing from Exchange.
func ValidateRecordTypeBoundary() error {
	classified := make(map[string]string, len(portableRecordTypes)+len(excludedRecordTypes))
	for _, recordType := range portableRecordTypes {
		if strings.TrimSpace(recordType) != recordType || !resourceTypePattern.MatchString(recordType) {
			return fmt.Errorf("%w: invalid portable record type %q", ErrInvalidInput, recordType)
		}
		if previous := classified[recordType]; previous != "" {
			return fmt.Errorf("%w: record type %q is classified as both %s and portable", ErrConflict, recordType, previous)
		}
		classified[recordType] = "portable"
	}
	for _, exclusion := range excludedRecordTypes {
		if strings.TrimSpace(exclusion.Type) != exclusion.Type || !resourceTypePattern.MatchString(exclusion.Type) || strings.TrimSpace(exclusion.Reason) == "" {
			return fmt.Errorf("%w: invalid excluded record type %q", ErrInvalidInput, exclusion.Type)
		}
		if previous := classified[exclusion.Type]; previous != "" {
			return fmt.Errorf("%w: record type %q is classified as both %s and excluded", ErrConflict, exclusion.Type, previous)
		}
		classified[exclusion.Type] = "excluded"
	}
	core := patterns.CoreRecordTypes()
	if len(classified) != len(core) {
		return fmt.Errorf("%w: Exchange classifies %d record types but Patterns defines %d", ErrConflict, len(classified), len(core))
	}
	for _, recordType := range core {
		if classified[recordType] == "" {
			return fmt.Errorf("%w: Patterns record type %q has no Exchange classification", ErrConflict, recordType)
		}
	}
	return nil
}

// ValidatePortableProviders proves that the application registered one and
// only one provider owner for every portable record family and no provider for
// a deliberate exclusion.
func ValidatePortableProviders(providers ...Provider) error {
	if err := ValidateRecordTypeBoundary(); err != nil {
		return err
	}
	want := PortableRecordTypes()
	seen := make(map[string]struct{}, len(want))
	for _, provider := range providers {
		if provider == nil {
			return errors.New("Exchange provider is required")
		}
		for _, recordType := range provider.Types() {
			if !slices.Contains(want, recordType) {
				return fmt.Errorf("%w: Exchange provider owns non-portable record type %q", ErrInvalidInput, recordType)
			}
			if _, duplicate := seen[recordType]; duplicate {
				return fmt.Errorf("%w: multiple Exchange providers own %q", ErrConflict, recordType)
			}
			seen[recordType] = struct{}{}
		}
	}
	for _, recordType := range want {
		if _, exists := seen[recordType]; !exists {
			return fmt.Errorf("%w: portable record type %q has no Exchange provider", ErrConflict, recordType)
		}
	}
	return nil
}
