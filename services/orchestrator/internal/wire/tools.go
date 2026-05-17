package wire

import (
	"log/slog"

	natslib "github.com/nats-io/nats.go"

	"github.com/f1xgun/onevoice/pkg/a2a"
	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/pkg/llm"
	"github.com/f1xgun/onevoice/services/orchestrator/internal/config"
	"github.com/f1xgun/onevoice/services/orchestrator/internal/natsexec"
	"github.com/f1xgun/onevoice/services/orchestrator/internal/toolregistry"
)

// toolSpec binds a tool definition to its ToolFloor baseline and
// per-tool EditableFields allowlist.
//
// Policy guidelines for choosing a floor:
//   - ToolFloorAuto       — read-only / safe queries (no external side effects).
//   - ToolFloorManual     — any public mutation (post, reply, update, schedule,
//     upload). Editable allowlist covers ONLY
//     human-facing text fields (text/caption/description);
//     ids, recipients, URLs, dates, categories, and
//     quantities are pinned at pause time.
//   - ToolFloorForbidden  — reserved for actions that must NEVER be lifted
//     via settings (e.g., a future "wipe all posts").
//     Kept registered so the LLM sees it exists but
//     policy.Resolve always denies. Destructive-but-
//     legitimate operations (comment moderation, etc.)
//     belong under Manual, not Forbidden — users with
//     a valid use-case can opt into auto-approval.
//
// When in doubt, prefer manual + a narrow editable list (conservative default).
type toolSpec struct {
	def         llm.ToolDefinition
	displayName string
	// displayNameKey is the i18n catalog key (under
	// `agentTasks.displayName.tools.<platform>.<action>.name` on the FE)
	// that the frontend uses to render localized task titles. The legacy
	// `displayName` stays as the source-of-truth literal — the FE renders
	// `t(displayNameKey) || displayName`. Phase C3 added the field.
	displayNameKey string
	// descriptionEn is the English translation of def.Function.Description
	// (which stays Russian as the source-of-truth literal). Resolved by
	// Registry.AvailableForLocale / AllEntriesForLocale at call time based
	// on the request's language tag (Phase D3 of `.planning/i18n-readiness/
	// PLAN.md`). Parameter descriptions inside def.Function.Parameters stay
	// RU-only for now — see deferred TODO note in Phase D3 plan.
	descriptionEn   string
	userDescription string // end-user-facing copy for /settings/tools (NO tool-name refs, NO "используй X вместо Y" disambiguation).
	floor           domain.ToolFloor
	editable        []string
}

// Tools constructs the live tool registry, dials NATS, and registers every
// MVP platform tool. NATS unreachable is non-fatal: the registry is returned
// with no platform tools registered (so the orchestrator boots and serves
// `/health/ready`), and the returned *natslib.Conn is nil — cmd/main.go
// branches on nil to skip the NATS health check and the deferred Drain().
//
// No ctx parameter: the v1 nats.go API does not accept a context for dial,
// so threading one through would be ceremonial. Re-add when the dial path
// becomes context-aware.
func Tools(log *slog.Logger, cfg *config.Config) (*toolregistry.Registry, *natslib.Conn, error) {
	reg := toolregistry.NewRegistry()

	nc, err := natslib.Connect(cfg.NATSUrl)
	if err != nil {
		log.Warn("NATS unavailable — tools will return stubs", "url", cfg.NATSUrl, "error", err)
		return reg, nil, nil
	}
	log.Info("connected to NATS", "url", cfg.NATSUrl)
	RegisterPlatformTools(reg, nc)
	return reg, nc, nil
}

// RegisterPlatformTools wires NATS executors into the tool registry for each
// MVP agent. MVP platforms: Telegram (API), VK (API), Yandex.Business (RPA),
// Google Business (API). Each platform's tool list lives in a sibling
// tools_{platform}.go file to keep individual files under the 500-LOC
// budget; this dispatcher is the single registration entry point.
//
// Every tool registration is explicit — Register takes floor + editableFields
// as required arguments so a newly-added tool can never silently inherit
// ToolFloorAuto. See toolSpec above for the policy rubric used below.
func RegisterPlatformTools(reg *toolregistry.Registry, nc *natslib.Conn) {
	agents := []struct {
		id    a2a.AgentID
		tools []toolSpec
	}{
		{id: a2a.AgentTelegram, tools: telegramTools()},
		{id: a2a.AgentVK, tools: vkTools()},
		{id: a2a.AgentYandexBusiness, tools: yandexTools()},
		{id: a2a.AgentGoogleBusiness, tools: googleTools()},
	}

	conn := natsexec.NewNATSConn(nc)
	for _, a := range agents {
		for _, spec := range a.tools {
			exec := natsexec.New(a.id, spec.def.Function.Name, conn)
			reg.Register(spec.def, spec.displayName, exec, spec.floor, spec.editable)
			if spec.userDescription != "" {
				reg.SetUserDescription(spec.def.Function.Name, spec.userDescription)
			}
			if spec.displayNameKey != "" {
				reg.SetDisplayNameKey(spec.def.Function.Name, spec.displayNameKey)
			}
			if spec.descriptionEn != "" {
				reg.SetDescriptionEn(spec.def.Function.Name, spec.descriptionEn)
			}
		}
	}
}
