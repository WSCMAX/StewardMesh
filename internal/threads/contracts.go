// Package threads implements organization-scoped tags, goals, and their
// relationships. Requirement: REQ-THREADS-001. Feature: goals.tags.
package threads

import (
	"context"
	"errors"
	"time"
)

const (
	RequirementID = "REQ-THREADS-001"
	FeatureID     = "goals.tags"
)

var (
	ErrInvalidInput = errors.New("invalid Threads input")
	ErrNotFound     = errors.New("Threads record not found")
	ErrConflict     = errors.New("Threads record conflicts with existing data")
	ErrCycle        = errors.New("Threads hierarchy would contain a cycle")
)

type TargetType string

const (
	TargetAsset    TargetType = "asset"
	TargetPurchase TargetType = "purchase"
	TargetContract TargetType = "contract"
	TargetSoftware TargetType = "software"
	TargetBudget   TargetType = "budget"
	TargetGoal     TargetType = "goal"
)

type RuleMode string

const (
	RuleInclude  RuleMode = "include"
	RuleSuppress RuleMode = "suppress"
)

type Tag struct {
	ID               string    `json:"id"`
	OrganizationID   string    `json:"organizationId"`
	Name             string    `json:"name"`
	ParentID         string    `json:"parentId,omitempty"`
	InheritByDefault bool      `json:"inheritByDefault"`
	Revision         int64     `json:"revision"`
	CreatedAt        time.Time `json:"createdAt"`
	UpdatedAt        time.Time `json:"updatedAt"`
}

type Goal struct {
	ID             string    `json:"id"`
	OrganizationID string    `json:"organizationId"`
	Name           string    `json:"name"`
	Description    string    `json:"description,omitempty"`
	ParentID       string    `json:"parentId,omitempty"`
	Revision       int64     `json:"revision"`
	CreatedAt      time.Time `json:"createdAt"`
	UpdatedAt      time.Time `json:"updatedAt"`
}

type TagRule struct {
	OrganizationID string     `json:"organizationId"`
	TargetType     TargetType `json:"targetType"`
	TargetID       string     `json:"targetId"`
	TagID          string     `json:"tagId"`
	Mode           RuleMode   `json:"mode"`
	Revision       int64      `json:"revision"`
	UpdatedBy      string     `json:"updatedBy"`
	CreatedAt      time.Time  `json:"createdAt"`
	UpdatedAt      time.Time  `json:"updatedAt"`
}

type EffectiveTag struct {
	Tag         Tag      `json:"tag"`
	State       string   `json:"state"`
	SourceTagID string   `json:"sourceTagId,omitempty"`
	Rule        *TagRule `json:"rule,omitempty"`
}

type GoalLink struct {
	OrganizationID string     `json:"organizationId"`
	GoalID         string     `json:"goalId"`
	TargetType     TargetType `json:"targetType"`
	TargetID       string     `json:"targetId"`
	CreatedBy      string     `json:"createdBy"`
	CreatedAt      time.Time  `json:"createdAt"`
}

type CreateTagInput struct {
	ID               string `json:"id,omitempty"`
	Name             string `json:"name"`
	ParentID         string `json:"parentId,omitempty"`
	InheritByDefault bool   `json:"inheritByDefault"`
}

type UpdateTagInput struct {
	ID               string `json:"-"`
	Name             string `json:"name"`
	ParentID         string `json:"parentId,omitempty"`
	InheritByDefault bool   `json:"inheritByDefault"`
	Revision         int64  `json:"revision"`
}

type CreateGoalInput struct {
	ID          string `json:"id,omitempty"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	ParentID    string `json:"parentId,omitempty"`
}

type UpdateGoalInput struct {
	ID          string `json:"-"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	ParentID    string `json:"parentId,omitempty"`
	Revision    int64  `json:"revision"`
}

type SetTagRuleInput struct {
	TargetType TargetType `json:"targetType"`
	TargetID   string     `json:"targetId"`
	TagID      string     `json:"tagId"`
	Mode       RuleMode   `json:"mode"`
	Revision   int64      `json:"revision"`
}

type LinkGoalInput struct {
	GoalID     string     `json:"goalId"`
	TargetType TargetType `json:"targetType"`
	TargetID   string     `json:"targetId"`
}

type Store interface {
	ListTags(ctx context.Context, organizationID string) ([]Tag, error)
	GetTag(ctx context.Context, organizationID, id string) (Tag, error)
	CreateTag(ctx context.Context, tag Tag) (Tag, error)
	UpdateTag(ctx context.Context, tag Tag, expectedRevision int64) (Tag, error)

	ListGoals(ctx context.Context, organizationID string) ([]Goal, error)
	GetGoal(ctx context.Context, organizationID, id string) (Goal, error)
	CreateGoal(ctx context.Context, goal Goal) (Goal, error)
	UpdateGoal(ctx context.Context, goal Goal, expectedRevision int64) (Goal, error)

	ListTagRules(ctx context.Context, organizationID string, targetType TargetType, targetID string) ([]TagRule, error)
	PutTagRule(ctx context.Context, rule TagRule, expectedRevision int64) (TagRule, error)
	DeleteTagRule(ctx context.Context, organizationID string, targetType TargetType, targetID, tagID string, expectedRevision int64) error

	ListGoalLinks(ctx context.Context, organizationID string, targetType TargetType, targetID string) ([]GoalLink, error)
	CreateGoalLink(ctx context.Context, link GoalLink) (GoalLink, bool, error)
	DeleteGoalLink(ctx context.Context, organizationID string, targetType TargetType, targetID, goalID string) (bool, error)
}

// TargetValidator verifies existing feature-owned targets without teaching
// Threads how those records are persisted.
type TargetValidator interface {
	ValidateThreadTarget(ctx context.Context, organizationID string, targetType TargetType, targetID string) error
}
