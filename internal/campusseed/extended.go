package campusseed

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/maxlemke/stewardmesh/internal/atlas"
	"github.com/maxlemke/stewardmesh/internal/atlascodes"
	"github.com/maxlemke/stewardmesh/internal/bridge"
	"github.com/maxlemke/stewardmesh/internal/directoryexpansion"
	"github.com/maxlemke/stewardmesh/internal/domain"
	"github.com/maxlemke/stewardmesh/internal/exchange"
	"github.com/maxlemke/stewardmesh/internal/foundation"
	"github.com/maxlemke/stewardmesh/internal/labels"
	"github.com/maxlemke/stewardmesh/internal/ledger"
	"github.com/maxlemke/stewardmesh/internal/patterns"
	"github.com/maxlemke/stewardmesh/internal/people"
	"github.com/maxlemke/stewardmesh/internal/reach"
	"github.com/maxlemke/stewardmesh/internal/signals"
	"github.com/maxlemke/stewardmesh/internal/stack"
	"github.com/maxlemke/stewardmesh/internal/threads"
)

const (
	campusDemoSignalsRulePrefix = "campus-demo-"
	campusDemoExchangeHoldingID = "campus-demo-stack-holding"
	campusDemoExchangeClaimedID = "campus-demo-stack-claimed"
)

var campusDemoBridgeClientID = stableID("bridge-client", "campus-demo-mcp")

type ExtendedDependencies struct {
	OrganizationID         string
	People                 *people.Service
	Atlas                  *atlas.Service
	Stack                  *stack.Service
	Ledger                 *ledger.Service
	Threads                *threads.Service
	Labels                 *labels.Service
	Signals                *signals.Service
	Reach                  *reach.Service
	BridgeStore            bridge.Store
	ExchangeStore          exchange.Store
	DirectoryGroups        directoryexpansion.GroupTargetStore
	AtlasLabels            *atlascodes.LabelService
	AtlasCodes             *atlascodes.Service
	Patterns               *patterns.Service
	ReachWebhookEndpointID string
	Auditor                foundation.Auditor
}

type ExtendedResult struct {
	AlreadySeeded bool
	TagCount      int
	GoalCount     int
	LabelCount    int
	RuleCount     int
	AlertCount    int
	GroupCount    int
}

type ExtendedSeeder struct {
	organizationID         string
	people                 *people.Service
	atlas                  *atlas.Service
	stack                  *stack.Service
	ledger                 *ledger.Service
	threads                *threads.Service
	labels                 *labels.Service
	signals                *signals.Service
	reach                  *reach.Service
	bridgeStore            bridge.Store
	exchangeStore          exchange.Store
	directoryGroups        directoryexpansion.GroupTargetStore
	atlasLabels            *atlascodes.LabelService
	atlasCodes             *atlascodes.Service
	patterns               *patterns.Service
	reachWebhookEndpointID string
	auditor                foundation.Auditor
	now                    func() time.Time

	siteID        string
	departmentIDs map[string]string
	roomIDs       map[string]string
	labAssetIDs   map[string][]string
	productIDs    map[string]string
	licenseIDs    map[string]string
	contractIDs   map[string]string
	budgetIDs     map[string]string
	identifierIDs map[string]string
	tagIDs        map[string]string
	goalIDs       map[string]string
}

func SeedExtended(ctx context.Context, dependencies ExtendedDependencies) (ExtendedResult, error) {
	seeder := ExtendedSeeder{
		organizationID:         strings.TrimSpace(dependencies.OrganizationID),
		people:                 dependencies.People,
		atlas:                  dependencies.Atlas,
		stack:                  dependencies.Stack,
		ledger:                 dependencies.Ledger,
		threads:                dependencies.Threads,
		labels:                 dependencies.Labels,
		signals:                dependencies.Signals,
		reach:                  dependencies.Reach,
		bridgeStore:            dependencies.BridgeStore,
		exchangeStore:          dependencies.ExchangeStore,
		directoryGroups:        dependencies.DirectoryGroups,
		atlasLabels:            dependencies.AtlasLabels,
		atlasCodes:             dependencies.AtlasCodes,
		patterns:               dependencies.Patterns,
		reachWebhookEndpointID: strings.TrimSpace(dependencies.ReachWebhookEndpointID),
		auditor:                dependencies.Auditor,
		now:                    func() time.Time { return time.Now().UTC() },
		departmentIDs:          make(map[string]string),
		roomIDs:                make(map[string]string),
		labAssetIDs:            make(map[string][]string),
		productIDs:             make(map[string]string),
		licenseIDs:             make(map[string]string),
		contractIDs:            make(map[string]string),
		budgetIDs:              make(map[string]string),
		identifierIDs:          make(map[string]string),
		tagIDs:                 make(map[string]string),
		goalIDs:                make(map[string]string),
	}
	return seeder.run(ctx)
}

func (s *ExtendedSeeder) run(ctx context.Context) (ExtendedResult, error) {
	result := ExtendedResult{}
	organizationID := strings.ToLower(s.organizationID)
	if ctx == nil || !strings.HasPrefix(organizationID, "demo-") ||
		s.people == nil || s.atlas == nil || s.stack == nil || s.ledger == nil ||
		s.threads == nil || s.labels == nil || s.signals == nil || s.reach == nil ||
		s.bridgeStore == nil || s.exchangeStore == nil || s.directoryGroups == nil || s.auditor == nil {
		return ExtendedResult{}, errors.New("campus extended seeding requires a demo-* organization and initialized Threads, Labels, Signals, Reach, Bridge, Exchange, and directory group services")
	}
	correlationID, err := foundation.NewCorrelationID()
	if err != nil {
		return ExtendedResult{}, fmt.Errorf("create campus extended correlation id: %w", err)
	}
	ctx = foundation.WithScope(ctx, foundation.Scope{
		OrganizationID: s.organizationID,
		ActorID:        ActorID,
		CorrelationID:  correlationID,
	})
	if seeded, err := s.alreadyExtendedSeeded(ctx); err != nil {
		return ExtendedResult{}, err
	} else if seeded {
		result.AlreadySeeded = true
		return result, nil
	}
	if err := s.resolveExistingState(ctx); err != nil {
		return ExtendedResult{}, err
	}
	if err := s.seedThreads(ctx); err != nil {
		return ExtendedResult{}, err
	}
	result.TagCount = len(s.tagIDs)
	result.GoalCount = len(s.goalIDs)
	if err := s.seedLabelDefinitions(ctx); err != nil {
		return ExtendedResult{}, err
	}
	result.LabelCount = 3
	if err := s.seedReach(ctx); err != nil {
		return ExtendedResult{}, err
	}
	ruleCount, alertCount, err := s.seedSignals(ctx)
	if err != nil {
		return ExtendedResult{}, err
	}
	result.RuleCount = ruleCount
	result.AlertCount = alertCount
	groupCount, err := s.seedDirectoryGroups(ctx)
	if err != nil {
		return ExtendedResult{}, err
	}
	result.GroupCount = groupCount
	if err := s.seedBridgeClient(ctx); err != nil {
		return ExtendedResult{}, err
	}
	if err := s.seedExchangePackages(ctx); err != nil {
		return ExtendedResult{}, err
	}
	if err := s.seedLabelBatch(ctx); err != nil {
		return ExtendedResult{}, err
	}
	if err := s.auditExtendedComplete(ctx, result); err != nil {
		return ExtendedResult{}, err
	}
	return result, nil
}

func (s *ExtendedSeeder) alreadyExtendedSeeded(ctx context.Context) (bool, error) {
	if _, err := s.bridgeStore.GetClient(ctx, s.organizationID, campusDemoBridgeClientID); err == nil {
		return true, nil
	} else if !errors.Is(err, bridge.ErrNotFound) {
		return false, fmt.Errorf("lookup campus demo bridge client: %w", err)
	}
	rules, err := s.signals.ListRules(ctx)
	if err != nil {
		return false, fmt.Errorf("list campus demo signals rules: %w", err)
	}
	for _, rule := range rules {
		if strings.HasPrefix(rule.ID, campusDemoSignalsRulePrefix) {
			return true, nil
		}
	}
	return false, nil
}

func (s *ExtendedSeeder) resolveExistingState(ctx context.Context) error {
	sites, err := s.people.ListSites(ctx, people.Visibility{All: true})
	if err != nil {
		return fmt.Errorf("list campus demo sites: %w", err)
	}
	for _, site := range sites {
		if strings.EqualFold(site.Name, SiteName) {
			s.siteID = site.ID
			break
		}
	}
	departments, err := s.people.ListDepartments(ctx, people.Visibility{All: true})
	if err != nil {
		return fmt.Errorf("list campus demo departments: %w", err)
	}
	for _, department := range departments {
		for _, definition := range campusDepartments {
			if strings.EqualFold(department.Name, definition.Name) {
				s.departmentIDs[definition.Slug] = department.ID
			}
		}
	}
	for _, lab := range campusLabs {
		rooms, listErr := s.people.ListRooms(ctx, s.siteID, "", people.Visibility{All: true})
		if listErr != nil {
			return listErr
		}
		for _, room := range rooms {
			if strings.EqualFold(room.Number, lab.RoomNumber) && strings.EqualFold(room.Name, lab.Name) {
				s.roomIDs[lab.Slug] = room.ID
				break
			}
		}
	}
	assets := make([]domain.Asset, 0, 2048)
	cursor := ""
	for {
		page, listErr := s.atlas.ListAssetsPage(ctx, atlas.Query{Limit: 100, Cursor: cursor})
		if listErr != nil {
			return fmt.Errorf("list campus demo assets: %w", listErr)
		}
		assets = append(assets, page.Items...)
		if page.NextCursor == "" {
			break
		}
		cursor = page.NextCursor
	}
	for _, asset := range assets {
		for _, lab := range campusLabs {
			prefix := lab.Slug + "-station-"
			if strings.HasPrefix(asset.ID, prefix) {
				s.labAssetIDs[lab.Slug] = append(s.labAssetIDs[lab.Slug], asset.ID)
			}
		}
	}
	stackSnapshot, err := s.stack.Snapshot(ctx)
	if err != nil {
		return fmt.Errorf("snapshot campus demo stack: %w", err)
	}
	for _, product := range stackSnapshot.Products {
		s.productIDs[product.ID] = product.ID
	}
	for _, license := range stackSnapshot.Licenses {
		s.licenseIDs[license.ID] = license.ID
	}
	ledgerSnapshot, err := s.ledger.Snapshot(ctx)
	if err != nil {
		return fmt.Errorf("snapshot campus demo ledger: %w", err)
	}
	for _, contract := range ledgerSnapshot.Contracts {
		s.contractIDs[contract.ID] = contract.ID
	}
	for _, budget := range ledgerSnapshot.Budgets {
		s.budgetIDs[budget.ID] = budget.ID
	}
	return nil
}

func (s *ExtendedSeeder) auditExtendedComplete(ctx context.Context, result ExtendedResult) error {
	eventID := stableID("audit", "campus-extended-seeded")
	event := foundation.AuditEvent{
		ID: eventID, OrganizationID: s.organizationID, ActorID: ActorID, CorrelationID: eventID,
		Action: "campus_demo.extended_seeded", ResourceType: "campus_demo_dataset", ResourceID: s.siteID,
		OccurredAt: s.now(),
		Metadata: map[string]string{
			"sourceSystemId": SourceSystemID,
			"tags":           fmt.Sprintf("%d", result.TagCount),
			"goals":          fmt.Sprintf("%d", result.GoalCount),
			"labels":         fmt.Sprintf("%d", result.LabelCount),
			"rules":          fmt.Sprintf("%d", result.RuleCount),
			"alerts":         fmt.Sprintf("%d", result.AlertCount),
			"groups":         fmt.Sprintf("%d", result.GroupCount),
		},
	}
	return s.auditor.Record(ctx, event)
}
