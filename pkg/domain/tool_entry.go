package domain

// ToolEntry is the canonical per-tool projection shared by:
//
//   - services/orchestrator/internal/toolregistry — produces it from the live
//     Registry (Registry.AllEntries / AllEntriesForLocale).
//   - services/api/internal/service — consumes it after fetching the
//     orchestrator's /internal/tools JSON via pkg/orchestratorclient and
//     materializing into the typed shape. Cached in ToolsRegistryCache.
//
// Before this consolidation, the same JSON shape was redefined separately in
// each location (toolregistry.RegistryEntry + service.ToolsRegistryEntry).
// They had identical fields and identical JSON tags but were distinct Go
// types because Go's internal/ visibility rule made cross-import impossible.
// Both now alias this single type, killing the drift risk.
//
// Lives in pkg/domain because it carries ToolFloor — a domain enum.
// Co-locating with pkg/tools would create an import cycle (pkg/domain's
// tests already reference pkg/tools name constants).
//
// DisplayNameKey is the i18n catalog key the frontend uses to render the
// tool label in the user's locale. Optional — older orchestrator deploys
// send "" and the FE falls back to DisplayName.
//
// Description is the LLM-facing copy (may reference other tool names and
// disambiguation rules). UserDescription is the end-user-facing copy shown
// in /settings/tools — guaranteed to never reference other tool names.
type ToolEntry struct {
	Name            string    `json:"name"`
	DisplayName     string    `json:"displayName"`
	DisplayNameKey  string    `json:"displayNameKey,omitempty"`
	Platform        string    `json:"platform"`
	Floor           ToolFloor `json:"floor"`
	EditableFields  []string  `json:"editableFields"`
	Description     string    `json:"description"`     // LLM-facing.
	UserDescription string    `json:"userDescription"` // end-user-facing copy for settings UI.
}
