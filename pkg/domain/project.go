package domain

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// WhitelistMode is a typed enum that eliminates null-vs-empty ambiguity in
// project tool whitelists (see .planning/research/PITFALLS.md §10).
type WhitelistMode string

const (
	WhitelistModeInherit  WhitelistMode = "inherit"  // use business defaults (v1.3 = all registered tools per D-18)
	WhitelistModeAll      WhitelistMode = "all"      // allow every registered tool for an active integration
	WhitelistModeExplicit WhitelistMode = "explicit" // allow only AllowedTools
	WhitelistModeNone     WhitelistMode = "none"     // allow nothing — LLM may respond but not act
)

// ValidWhitelistMode returns true if m is one of the defined modes.
func ValidWhitelistMode(m WhitelistMode) bool {
	switch m {
	case WhitelistModeInherit, WhitelistModeAll, WhitelistModeExplicit, WhitelistModeNone:
		return true
	}
	return false
}

// MaxProjectSystemPromptChars caps user-supplied system-prompt length.
// The user-facing copy is "4000 символов" (see 15-UI-SPEC.md line 140 and
// the Plan 05 ProjectForm counter). REQUIREMENTS.md PROJ-01 describes the
// intent as "max 1000 tokens"; this constant is the concrete character
// implementation (approx 4 chars/token) and is the invariant enforced at
// four layers: this constant, the Postgres CHECK constraint in
// 000003_projects.up.sql, the service-layer validator in Plan 03, and the
// frontend zod schema in Plan 05. All four MUST agree on 4000.
const MaxProjectSystemPromptChars = 4000

// QuickAction is a plain string. Kept as a named type alias so call sites
// self-document when they mean "a quick action string" versus any other
// string. Per D-54, v1.3 ships a plain string list; templating is deferred.
type QuickAction = string

// Project is a Postgres-backed grouping of chats with a shared system prompt
// override, tool whitelist, and quick-action list. Scoped per business.
type Project struct {
	ID            uuid.UUID     `json:"id" db:"id"`
	BusinessID    uuid.UUID     `json:"businessId" db:"business_id"`
	Name          string        `json:"name" db:"name"`
	Description   string        `json:"description" db:"description"`
	SystemPrompt  string        `json:"systemPrompt" db:"system_prompt"`
	WhitelistMode WhitelistMode `json:"whitelistMode" db:"whitelist_mode"`
	AllowedTools  []string      `json:"allowedTools" db:"allowed_tools"`
	QuickActions  []string      `json:"quickActions" db:"quick_actions"`
	CreatedAt     time.Time     `json:"createdAt" db:"created_at"`
	UpdatedAt     time.Time     `json:"updatedAt" db:"updated_at"`
}

// ProjectRepository is the contract implemented by services/api/internal/repository
// in Plan 03. HardDeleteCascade is part of the interface (not an out-of-band
// helper) so the service layer wires via a single interface type — no type
// assertions, no anonymous interface widening. Cascade order inside the
// implementation is mongo messages → mongo conversations → postgres project,
// which gives retry idempotence when the Postgres delete fails last.
type ProjectRepository interface {
	Create(ctx context.Context, project *Project) error
	GetByID(ctx context.Context, id uuid.UUID) (*Project, error)
	ListByBusinessID(ctx context.Context, businessID uuid.UUID) ([]Project, error)
	Update(ctx context.Context, project *Project) error
	Delete(ctx context.Context, id uuid.UUID) error
	CountConversationsByID(ctx context.Context, id uuid.UUID) (int, error)
	HardDeleteCascade(ctx context.Context, id uuid.UUID) (deletedConvos int, deletedMessages int, err error)
}
