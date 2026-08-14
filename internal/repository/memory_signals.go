package repository

// In-memory Signals adapter. Requirement: REQ-SIGNALS-001. Feature: alerts.rules. GitHub: #11.

import (
	"context"
	"reflect"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/maxlemke/stewardmesh/internal/signals"
)

type MemorySignalsStore struct {
	mu            sync.RWMutex
	rules         map[string]signals.Rule
	alerts        map[string]signals.Alert
	alertsByDedup map[string]string
	history       map[string][]signals.AlertHistory
	subscriptions map[string]signals.Subscription
	deliveries    map[string]signals.Delivery
}

func NewMemorySignalsStore() *MemorySignalsStore {
	return &MemorySignalsStore{rules: map[string]signals.Rule{}, alerts: map[string]signals.Alert{}, alertsByDedup: map[string]string{}, history: map[string][]signals.AlertHistory{}, subscriptions: map[string]signals.Subscription{}, deliveries: map[string]signals.Delivery{}}
}

func signalsKey(organizationID, id string) string { return organizationID + "\x00" + id }

func (s *MemorySignalsStore) ExchangeSnapshot(_ context.Context, organizationID string, maximum int) (signals.ExchangeSnapshot, error) {
	if maximum < 1 {
		return signals.ExchangeSnapshot{}, signals.ErrInvalidInput
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := signals.ExchangeSnapshot{Rules: []signals.Rule{}, Subscriptions: []signals.Subscription{}}
	for _, item := range s.rules {
		if item.OrganizationID == organizationID {
			result.Rules = append(result.Rules, cloneSignalRule(item))
		}
	}
	for _, item := range s.subscriptions {
		if item.OrganizationID == organizationID {
			result.Subscriptions = append(result.Subscriptions, item)
		}
	}
	if len(result.Rules)+len(result.Subscriptions) > maximum {
		return signals.ExchangeSnapshot{}, signals.ErrTooLarge
	}
	sort.Slice(result.Rules, func(i, j int) bool { return result.Rules[i].ID < result.Rules[j].ID })
	sort.Slice(result.Subscriptions, func(i, j int) bool { return result.Subscriptions[i].ID < result.Subscriptions[j].ID })
	return result, nil
}

func (s *MemorySignalsStore) ListRules(_ context.Context, organizationID string) ([]signals.Rule, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	items := []signals.Rule{}
	for _, item := range s.rules {
		if item.OrganizationID == organizationID {
			items = append(items, cloneSignalRule(item))
		}
	}
	sort.Slice(items, func(i, j int) bool {
		return items[i].Name < items[j].Name || items[i].Name == items[j].Name && items[i].ID < items[j].ID
	})
	return items, nil
}

func (s *MemorySignalsStore) GetRule(_ context.Context, organizationID, id string) (signals.Rule, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	item, ok := s.rules[signalsKey(organizationID, id)]
	if !ok {
		return signals.Rule{}, signals.ErrNotFound
	}
	return cloneSignalRule(item), nil
}

func (s *MemorySignalsStore) CreateRule(_ context.Context, item signals.Rule) (signals.Rule, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := signalsKey(item.OrganizationID, item.ID)
	if _, ok := s.rules[key]; ok {
		return signals.Rule{}, signals.ErrConflict
	}
	count := 0
	for _, existing := range s.rules {
		if existing.OrganizationID == item.OrganizationID {
			count++
			if strings.EqualFold(existing.Name, item.Name) {
				return signals.Rule{}, signals.ErrConflict
			}
		}
	}
	if count >= signals.MaximumRules {
		return signals.Rule{}, signals.ErrConflict
	}
	s.rules[key] = cloneSignalRule(item)
	return cloneSignalRule(item), nil
}

func (s *MemorySignalsStore) UpdateRule(_ context.Context, item signals.Rule, expectedRevision int64) (signals.Rule, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := signalsKey(item.OrganizationID, item.ID)
	existing, ok := s.rules[key]
	if !ok {
		return signals.Rule{}, signals.ErrNotFound
	}
	if existing.Revision != expectedRevision {
		return signals.Rule{}, signals.ErrConflict
	}
	for candidateKey, candidate := range s.rules {
		if candidateKey != key && candidate.OrganizationID == item.OrganizationID && strings.EqualFold(candidate.Name, item.Name) {
			return signals.Rule{}, signals.ErrConflict
		}
	}
	s.rules[key] = cloneSignalRule(item)
	return cloneSignalRule(item), nil
}

func (s *MemorySignalsStore) ImportRule(_ context.Context, item signals.Rule) (signals.Rule, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := signalsKey(item.OrganizationID, item.ID)
	if existing, ok := s.rules[key]; ok {
		return cloneSignalRule(existing), false, nil
	}
	count := 0
	for _, existing := range s.rules {
		if existing.OrganizationID == item.OrganizationID {
			count++
			if strings.EqualFold(existing.Name, item.Name) {
				return signals.Rule{}, false, signals.ErrConflict
			}
		}
	}
	if count >= signals.MaximumRules {
		return signals.Rule{}, false, signals.ErrConflict
	}
	s.rules[key] = cloneSignalRule(item)
	return cloneSignalRule(item), true, nil
}

func (s *MemorySignalsStore) ListAlerts(_ context.Context, organizationID string, query signals.AlertQuery) ([]signals.Alert, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	items := []signals.Alert{}
	for _, item := range s.alerts {
		if item.OrganizationID != organizationID || query.RuleID != "" && item.RuleID != query.RuleID || query.Status != "" && item.Status != query.Status || query.Severity != "" && item.Severity != query.Severity || query.Condition != "" && item.Condition != query.Condition {
			continue
		}
		items = append(items, cloneSignalAlert(item))
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].LastObservedAt.Equal(items[j].LastObservedAt) {
			return items[i].ID < items[j].ID
		}
		return items[i].LastObservedAt.After(items[j].LastObservedAt)
	})
	if len(items) > query.Limit {
		items = items[:query.Limit]
	}
	return items, nil
}

func (s *MemorySignalsStore) GetAlert(_ context.Context, organizationID, id string) (signals.Alert, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	item, ok := s.alerts[signalsKey(organizationID, id)]
	if !ok {
		return signals.Alert{}, signals.ErrNotFound
	}
	return cloneSignalAlert(item), nil
}

func (s *MemorySignalsStore) GetAlertByDeduplicationKey(_ context.Context, organizationID, dedup string) (signals.Alert, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	id, ok := s.alertsByDedup[signalsKey(organizationID, dedup)]
	if !ok {
		return signals.Alert{}, signals.ErrNotFound
	}
	return cloneSignalAlert(s.alerts[signalsKey(organizationID, id)]), nil
}

func (s *MemorySignalsStore) CreateAlert(_ context.Context, alert signals.Alert, history signals.AlertHistory) (signals.Alert, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key, dedupKey := signalsKey(alert.OrganizationID, alert.ID), signalsKey(alert.OrganizationID, alert.DeduplicationKey)
	if _, ok := s.alerts[key]; ok || s.alertsByDedup[dedupKey] != "" {
		return signals.Alert{}, signals.ErrConflict
	}
	s.alerts[key], s.alertsByDedup[dedupKey] = cloneSignalAlert(alert), alert.ID
	s.history[key] = append(s.history[key], history)
	return cloneSignalAlert(alert), nil
}

func (s *MemorySignalsStore) UpdateAlert(_ context.Context, alert signals.Alert, expectedRevision int64, history signals.AlertHistory) (signals.Alert, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := signalsKey(alert.OrganizationID, alert.ID)
	existing, ok := s.alerts[key]
	if !ok {
		return signals.Alert{}, signals.ErrNotFound
	}
	if existing.Revision != expectedRevision || existing.DeduplicationKey != alert.DeduplicationKey {
		return signals.Alert{}, signals.ErrConflict
	}
	s.alerts[key] = cloneSignalAlert(alert)
	s.history[key] = append(s.history[key], history)
	return cloneSignalAlert(alert), nil
}

func (s *MemorySignalsStore) ListAlertHistory(_ context.Context, organizationID, alertID string) ([]signals.AlertHistory, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	key := signalsKey(organizationID, alertID)
	if _, ok := s.alerts[key]; !ok {
		return nil, signals.ErrNotFound
	}
	items := append([]signals.AlertHistory(nil), s.history[key]...)
	sort.Slice(items, func(i, j int) bool { return items[i].Revision > items[j].Revision })
	return items, nil
}

func (s *MemorySignalsStore) ListSubscriptions(_ context.Context, organizationID string) ([]signals.Subscription, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	items := []signals.Subscription{}
	for _, item := range s.subscriptions {
		if item.OrganizationID == organizationID {
			items = append(items, item)
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	return items, nil
}

func (s *MemorySignalsStore) GetSubscription(_ context.Context, organizationID, id string) (signals.Subscription, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	item, ok := s.subscriptions[signalsKey(organizationID, id)]
	if !ok {
		return signals.Subscription{}, signals.ErrNotFound
	}
	return item, nil
}

func (s *MemorySignalsStore) CreateSubscription(_ context.Context, item signals.Subscription) (signals.Subscription, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key, count := signalsKey(item.OrganizationID, item.ID), 0
	if _, ok := s.subscriptions[key]; ok {
		return signals.Subscription{}, signals.ErrConflict
	}
	for _, existing := range s.subscriptions {
		if existing.OrganizationID == item.OrganizationID {
			count++
			if existing.RuleID == item.RuleID && existing.TargetKind == item.TargetKind && existing.TargetID == item.TargetID {
				return signals.Subscription{}, signals.ErrConflict
			}
		}
	}
	if count >= signals.MaximumSubscriptions {
		return signals.Subscription{}, signals.ErrConflict
	}
	s.subscriptions[key] = item
	return item, nil
}

func (s *MemorySignalsStore) ImportSubscription(_ context.Context, item signals.Subscription) (signals.Subscription, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key, count := signalsKey(item.OrganizationID, item.ID), 0
	if existing, ok := s.subscriptions[key]; ok {
		return existing, false, nil
	}
	for _, existing := range s.subscriptions {
		if existing.OrganizationID == item.OrganizationID {
			count++
			if existing.RuleID == item.RuleID && existing.TargetKind == item.TargetKind && existing.TargetID == item.TargetID {
				return signals.Subscription{}, false, signals.ErrConflict
			}
		}
	}
	if count >= signals.MaximumSubscriptions {
		return signals.Subscription{}, false, signals.ErrConflict
	}
	s.subscriptions[key] = item
	return item, true, nil
}

func (s *MemorySignalsStore) DeleteSubscription(_ context.Context, organizationID, id string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := signalsKey(organizationID, id)
	if _, ok := s.subscriptions[key]; !ok {
		return false, nil
	}
	delete(s.subscriptions, key)
	for deliveryKey, delivery := range s.deliveries {
		if delivery.OrganizationID == organizationID && delivery.SubscriptionID == id {
			delete(s.deliveries, deliveryKey)
		}
	}
	return true, nil
}

func (s *MemorySignalsStore) CreateDelivery(_ context.Context, item signals.Delivery) (signals.Delivery, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := signalsKey(item.OrganizationID, item.ID)
	if existing, ok := s.deliveries[key]; ok {
		if reflect.DeepEqual(existing, item) {
			return existing, false, nil
		}
		return signals.Delivery{}, false, signals.ErrConflict
	}
	s.deliveries[key] = cloneSignalDelivery(item)
	return cloneSignalDelivery(item), true, nil
}

func (s *MemorySignalsStore) ListPendingDeliveries(_ context.Context, organizationID string, asOf time.Time, limit int) ([]signals.Delivery, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	items := []signals.Delivery{}
	for _, item := range s.deliveries {
		if item.OrganizationID == organizationID && item.Status == "pending" && (item.NextAttemptAt == nil || !item.NextAttemptAt.After(asOf)) {
			items = append(items, cloneSignalDelivery(item))
		}
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].CreatedAt.Equal(items[j].CreatedAt) {
			return items[i].ID < items[j].ID
		}
		return items[i].CreatedAt.Before(items[j].CreatedAt)
	})
	if len(items) > limit {
		items = items[:limit]
	}
	return items, nil
}

func (s *MemorySignalsStore) UpdateDelivery(_ context.Context, item signals.Delivery, expectedAttempts int) (signals.Delivery, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := signalsKey(item.OrganizationID, item.ID)
	existing, ok := s.deliveries[key]
	if !ok {
		return signals.Delivery{}, signals.ErrNotFound
	}
	if existing.Attempts != expectedAttempts {
		return signals.Delivery{}, signals.ErrConflict
	}
	s.deliveries[key] = cloneSignalDelivery(item)
	return cloneSignalDelivery(item), nil
}

func cloneSignalRule(item signals.Rule) signals.Rule {
	// Preserve the API contract for thresholdless rules: an empty collection
	// serializes as [] rather than null while still isolating stored slices.
	item.ThresholdDays = append([]int{}, item.ThresholdDays...)
	return item
}

func cloneSignalAlert(item signals.Alert) signals.Alert {
	if item.DueAt != nil {
		value := *item.DueAt
		item.DueAt = &value
	}
	if item.AcknowledgedAt != nil {
		value := *item.AcknowledgedAt
		item.AcknowledgedAt = &value
	}
	if item.ResolvedAt != nil {
		value := *item.ResolvedAt
		item.ResolvedAt = &value
	}
	return item
}

func cloneSignalDelivery(item signals.Delivery) signals.Delivery {
	if item.NextAttemptAt != nil {
		value := *item.NextAttemptAt
		item.NextAttemptAt = &value
	}
	return item
}
