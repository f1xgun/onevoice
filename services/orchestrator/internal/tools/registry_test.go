package tools_test

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/pkg/llm"
	"github.com/f1xgun/onevoice/services/orchestrator/internal/tools"
)

func makeDef(name string) llm.ToolDefinition {
	return llm.ToolDefinition{
		Type:     "function",
		Function: llm.FunctionDefinition{Name: name, Description: "test", Parameters: map[string]interface{}{}},
	}
}

// newCaptureLogger swaps the default slog logger for one backed by a buffer
// so tests can assert slog.WarnContext output. The original logger is
// restored via t.Cleanup.
func newCaptureLogger(t *testing.T) *bytes.Buffer {
	t.Helper()
	buf := &bytes.Buffer{}
	handler := slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	prev := slog.Default()
	slog.SetDefault(slog.New(handler))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return buf
}

func TestRegistry_FilterByActiveIntegrations(t *testing.T) {
	reg := tools.NewRegistry()
	reg.Register(makeDef("telegram__send_post"), "", nil, domain.ToolFloorAuto, nil)
	reg.Register(makeDef("vk__publish_post"), "", nil, domain.ToolFloorAuto, nil)
	reg.Register(makeDef("google_business__update_hours"), "", nil, domain.ToolFloorAuto, nil)
	reg.Register(makeDef("get_business_info"), "", nil, domain.ToolFloorAuto, nil) // internal tool, always available

	active := []string{"telegram"}
	defs := reg.Available(active)

	names := make([]string, len(defs))
	for i, d := range defs {
		names[i] = d.Function.Name
	}
	assert.Contains(t, names, "telegram__send_post")
	assert.Contains(t, names, "get_business_info")
	assert.NotContains(t, names, "vk__publish_post")
	assert.NotContains(t, names, "google_business__update_hours")
}

func TestRegistry_NoActiveIntegrations_OnlyInternalTools(t *testing.T) {
	reg := tools.NewRegistry()
	reg.Register(makeDef("telegram__send_post"), "", nil, domain.ToolFloorAuto, nil)
	reg.Register(makeDef("get_business_info"), "", nil, domain.ToolFloorAuto, nil)

	defs := reg.Available(nil)

	assert.Len(t, defs, 1)
	assert.Equal(t, "get_business_info", defs[0].Function.Name)
}

func TestRegistry_Execute_CallsExecutor(t *testing.T) {
	reg := tools.NewRegistry()
	called := false
	executor := tools.ExecutorFunc(func(ctx context.Context, args map[string]interface{}) (interface{}, error) {
		called = true
		return map[string]interface{}{"ok": true}, nil
	})
	reg.Register(makeDef("telegram__send_post"), "", executor, domain.ToolFloorAuto, nil)

	result, err := reg.Execute(context.Background(), "telegram__send_post", map[string]interface{}{})
	require.NoError(t, err)
	assert.True(t, called)
	assert.NotNil(t, result)
}

func TestRegistry_Execute_UnknownTool(t *testing.T) {
	reg := tools.NewRegistry()
	_, err := reg.Execute(context.Background(), "unknown__tool", nil)
	assert.ErrorContains(t, err, "unknown tool")
}

// toolNames extracts the sorted set of tool names from a slice of definitions.
func toolNames(defs []llm.ToolDefinition) []string {
	out := make([]string, len(defs))
	for i, d := range defs {
		out[i] = d.Function.Name
	}
	return out
}

// fixtureRegistry returns a registry populated with a realistic mix of
// Manual-floor write tools (matches services/orchestrator/cmd/main.go's
// production registrations) plus Auto-floor read tools. The Auto/Manual
// split is the basis for "auto-floor read tools always available under
// ModeExplicit" — see AvailableForWhitelist's docstring.
func fixtureRegistry() *tools.Registry {
	reg := tools.NewRegistry()
	// Write tools — Manual floor, fully gated by whitelist + HITL.
	reg.Register(makeDef("telegram__send_channel_post"), "", nil, domain.ToolFloorManual, nil)
	reg.Register(makeDef("telegram__send_notification"), "", nil, domain.ToolFloorManual, nil)
	reg.Register(makeDef("vk__publish_post"), "", nil, domain.ToolFloorManual, nil)
	// Read tools — Auto floor, always available under ModeExplicit so the
	// LLM can fetch context (Pitfall: clicking "Проверить отзывы" with only
	// a write tool in whitelist made the LLM publish posts ABOUT checking
	// reviews instead of fetching them).
	reg.Register(makeDef("telegram__get_reviews"), "", nil, domain.ToolFloorAuto, nil)
	reg.Register(makeDef("vk__get_comments"), "", nil, domain.ToolFloorAuto, nil)
	// Internal — no platform prefix, always available.
	reg.Register(makeDef("get_business_info"), "", nil, domain.ToolFloorAuto, nil)
	return reg
}

func TestRegistry_AvailableForWhitelist_EmptyMode_SameAsAvailable(t *testing.T) {
	reg := fixtureRegistry()
	base := reg.Available([]string{"telegram", "vk"})
	got := reg.AvailableForWhitelist(context.Background(), []string{"telegram", "vk"}, "", nil)
	assert.ElementsMatch(t, toolNames(base), toolNames(got))
}

func TestRegistry_AvailableForWhitelist_ModeAll_SameAsAvailable(t *testing.T) {
	reg := fixtureRegistry()
	base := reg.Available([]string{"telegram", "vk"})
	got := reg.AvailableForWhitelist(context.Background(), []string{"telegram", "vk"}, domain.WhitelistModeAll, nil)
	assert.ElementsMatch(t, toolNames(base), toolNames(got))
}

func TestRegistry_AvailableForWhitelist_ModeInherit_SameAsAll(t *testing.T) {
	// D-18: for v1.3, inherit == all. Phase 16 replaces with business defaults.
	reg := fixtureRegistry()
	base := reg.Available([]string{"telegram", "vk"})
	got := reg.AvailableForWhitelist(context.Background(), []string{"telegram", "vk"}, domain.WhitelistModeInherit, nil)
	assert.ElementsMatch(t, toolNames(base), toolNames(got))
}

func TestRegistry_AvailableForWhitelist_ModeNone_Empty(t *testing.T) {
	reg := fixtureRegistry()
	got := reg.AvailableForWhitelist(context.Background(), []string{"telegram", "vk"}, domain.WhitelistModeNone, nil)
	assert.Empty(t, got)
}

func TestRegistry_AvailableForWhitelist_ModeExplicit_Intersection(t *testing.T) {
	reg := fixtureRegistry()
	got := reg.AvailableForWhitelist(
		context.Background(),
		[]string{"telegram", "vk"},
		domain.WhitelistModeExplicit,
		[]string{"telegram__send_channel_post"},
	)
	names := toolNames(got)
	// Explicit allowlist returns the named Manual-floor write tool PLUS
	// every Auto-floor read tool for active integrations (always-available
	// exemption — see AvailableForWhitelist docstring).
	assert.ElementsMatch(t,
		[]string{
			"telegram__send_channel_post", // explicitly allowed
			"telegram__get_reviews",       // Auto floor, telegram active
			"vk__get_comments",            // Auto floor, vk active
			"get_business_info",           // Auto floor, internal (no platform prefix)
		},
		names,
	)
}

func TestRegistry_AvailableForWhitelist_ModeExplicit_FiltersOutInactivePlatform(t *testing.T) {
	// VK whitelisted but VK not active → vk__publish_post dropped.
	// Auto-floor tools for the ACTIVE platform (telegram) still come through.
	reg := fixtureRegistry()
	got := reg.AvailableForWhitelist(
		context.Background(),
		[]string{"telegram"},
		domain.WhitelistModeExplicit,
		[]string{"vk__publish_post"},
	)
	names := toolNames(got)
	assert.NotContains(t, names, "vk__publish_post", "VK platform inactive")
	assert.NotContains(t, names, "vk__get_comments", "VK platform inactive")
	assert.NotContains(t, names, "telegram__send_channel_post", "Manual write tool not in allowlist")
	assert.ElementsMatch(t,
		[]string{
			"telegram__get_reviews", // Auto, telegram active
			"get_business_info",     // Auto, internal
		},
		names,
	)
}

func TestRegistry_AvailableForWhitelist_ModeExplicit_UnknownTool_LogsAndDrops(t *testing.T) {
	buf := newCaptureLogger(t)
	reg := fixtureRegistry()
	got := reg.AvailableForWhitelist(
		context.Background(),
		[]string{"telegram"},
		domain.WhitelistModeExplicit,
		[]string{"unknown__tool"},
	)
	names := toolNames(got)
	// Unknown tool dropped; no Manual-floor write tools come through;
	// Auto-floor read tools for active platform still available.
	assert.NotContains(t, names, "unknown__tool")
	assert.NotContains(t, names, "telegram__send_channel_post")
	assert.NotContains(t, names, "telegram__send_notification")
	assert.ElementsMatch(t,
		[]string{
			"telegram__get_reviews",
			"get_business_info",
		},
		names,
	)
	logs := buf.String()
	assert.Contains(t, logs, "project whitelist contains unknown tool")
	assert.Contains(t, logs, "unknown__tool")
}

func TestRegistry_AvailableForWhitelist_UnknownMode_FallsBackToInherit(t *testing.T) {
	buf := newCaptureLogger(t)
	reg := fixtureRegistry()
	base := reg.Available([]string{"telegram"})
	got := reg.AvailableForWhitelist(
		context.Background(),
		[]string{"telegram"},
		domain.WhitelistMode("bogus"),
		nil,
	)
	assert.ElementsMatch(t, toolNames(base), toolNames(got))
	assert.Contains(t, buf.String(), "unknown whitelist mode")
}

func TestRegistry_AvailableForWhitelist_ModeExplicit_MixedKnownAndUnknown(t *testing.T) {
	buf := newCaptureLogger(t)
	reg := fixtureRegistry()
	got := reg.AvailableForWhitelist(
		context.Background(),
		[]string{"telegram", "vk"},
		domain.WhitelistModeExplicit,
		[]string{"telegram__send_channel_post", "bogus__tool"},
	)
	names := toolNames(got)
	// Known tool + auto-floor exemptions; unknown dropped + logged.
	assert.ElementsMatch(t,
		[]string{
			"telegram__send_channel_post",
			"telegram__get_reviews",
			"vk__get_comments",
			"get_business_info",
		},
		names,
	)
	assert.NotContains(t, names, "bogus__tool")
	assert.True(t, strings.Contains(buf.String(), "bogus__tool"))
}

// TestRegistry_AvailableForWhitelist_ModeExplicit_AutoFloorAlwaysIncluded
// locks the read-tools-by-default contract: even with an EMPTY allowlist,
// every Auto-floor tool for the active integrations is exposed to the LLM.
// Quick-actions like "Проверить отзывы" only work if get_*-style tools can
// be called regardless of the explicit whitelist (which is intended to gate
// write actions, not read).
func TestRegistry_AvailableForWhitelist_ModeExplicit_AutoFloorAlwaysIncluded(t *testing.T) {
	reg := fixtureRegistry()
	got := reg.AvailableForWhitelist(
		context.Background(),
		[]string{"telegram", "vk"},
		domain.WhitelistModeExplicit,
		nil, // empty allowlist — only auto-floor tools should come through
	)
	names := toolNames(got)
	assert.NotContains(t, names, "telegram__send_channel_post", "Manual floor must require explicit whitelist")
	assert.NotContains(t, names, "telegram__send_notification", "Manual floor must require explicit whitelist")
	assert.NotContains(t, names, "vk__publish_post", "Manual floor must require explicit whitelist")
	assert.ElementsMatch(t,
		[]string{
			"telegram__get_reviews",
			"vk__get_comments",
			"get_business_info",
		},
		names,
	)
}

// TestRegistry_AvailableForWhitelist_ModeNone_BlocksEverythingIncludingAuto
// locks the absolute-stop semantics of WhitelistModeNone: when the operator
// explicitly says "no tools at all", we honor it — auto-floor read tools
// do NOT bypass this. The exemption only applies under ModeExplicit, where
// the whitelist is a positive allowlist for write tools.
func TestRegistry_AvailableForWhitelist_ModeNone_BlocksEverythingIncludingAuto(t *testing.T) {
	reg := fixtureRegistry()
	got := reg.AvailableForWhitelist(
		context.Background(),
		[]string{"telegram", "vk"},
		domain.WhitelistModeNone,
		[]string{"telegram__get_reviews"}, // even allowlisting an auto tool doesn't matter
	)
	assert.Empty(t, got)
}

// --- Phase 16 Plan 16-03 additions: Floor, EditableFields, Has, AllEntries ---

func TestRegistry_Floor_RegisteredReturnsFloor(t *testing.T) {
	reg := tools.NewRegistry()
	reg.Register(makeDef("telegram__send_channel_post"), "", nil, domain.ToolFloorManual, []string{"text"})
	reg.Register(makeDef("telegram__get_reviews"), "", nil, domain.ToolFloorAuto, nil)

	assert.Equal(t, domain.ToolFloorManual, reg.Floor("telegram__send_channel_post"))
	assert.Equal(t, domain.ToolFloorAuto, reg.Floor("telegram__get_reviews"))
}

// TestRegistry_Floor_UnknownReturnsForbidden locks POLICY-07's safe-default:
// the runtime policy resolver treats unknown tools as if they were registered
// with Floor=Forbidden. Changing this to Auto or Manual would silently permit
// approval of tools that no longer exist.
func TestRegistry_Floor_UnknownReturnsForbidden(t *testing.T) {
	reg := tools.NewRegistry()
	reg.Register(makeDef("telegram__send_channel_post"), "", nil, domain.ToolFloorAuto, nil)

	assert.Equal(t, domain.ToolFloorForbidden, reg.Floor("ghost__missing"))
	assert.Equal(t, domain.ToolFloorForbidden, reg.Floor(""))
}

func TestRegistry_EditableFields_RegisteredReturnsList(t *testing.T) {
	reg := tools.NewRegistry()
	reg.Register(
		makeDef("telegram__send_channel_post"),
		"",
		nil,
		domain.ToolFloorManual,
		[]string{"text", "parse_mode"},
	)
	got := reg.EditableFields("telegram__send_channel_post")
	assert.ElementsMatch(t, []string{"text", "parse_mode"}, got)
}

func TestRegistry_EditableFields_UnknownReturnsNil(t *testing.T) {
	reg := tools.NewRegistry()
	assert.Nil(t, reg.EditableFields("ghost__missing"))
}

// TestRegistry_EditableFields_Defensive verifies that mutating the slice
// returned by EditableFields does not mutate registry state. Without the
// defensive copy in Register + EditableFields, a caller could widen the
// allowlist at runtime by appending to the returned slice.
func TestRegistry_EditableFields_Defensive(t *testing.T) {
	reg := tools.NewRegistry()
	original := []string{"text", "parse_mode"}
	reg.Register(makeDef("telegram__send_channel_post"), "", nil, domain.ToolFloorManual, original)

	// Mutate the caller's slice after Register — registry should not observe the change.
	original[0] = "channel_id"
	got := reg.EditableFields("telegram__send_channel_post")
	assert.ElementsMatch(t, []string{"text", "parse_mode"}, got)

	// Mutate the returned slice — registry should not observe the change.
	got[0] = "tampered"
	fresh := reg.EditableFields("telegram__send_channel_post")
	assert.ElementsMatch(t, []string{"text", "parse_mode"}, fresh)
}

func TestRegistry_Has(t *testing.T) {
	reg := tools.NewRegistry()
	reg.Register(makeDef("telegram__send_channel_post"), "", nil, domain.ToolFloorManual, nil)

	assert.True(t, reg.Has("telegram__send_channel_post"))
	assert.False(t, reg.Has("ghost__missing"))
	assert.False(t, reg.Has(""))
}

func TestRegistry_AllFloors(t *testing.T) {
	reg := tools.NewRegistry()
	reg.Register(makeDef("telegram__send_channel_post"), "", nil, domain.ToolFloorManual, []string{"text"})
	reg.Register(makeDef("telegram__get_reviews"), "", nil, domain.ToolFloorAuto, nil)
	reg.Register(makeDef("dangerous__delete"), "", nil, domain.ToolFloorForbidden, nil)

	got := reg.AllFloors()
	assert.Equal(t, domain.ToolFloorManual, got["telegram__send_channel_post"])
	assert.Equal(t, domain.ToolFloorAuto, got["telegram__get_reviews"])
	assert.Equal(t, domain.ToolFloorForbidden, got["dangerous__delete"])
	assert.Len(t, got, 3)
}

// TestRegistry_AllEntries_SplitsPlatform checks that the platform prefix is
// correctly derived from "{platform}__{action}" and that bare (internal) tools
// map to an empty platform. Guards the edge cases spelled out in the plan:
//   - "telegram__send_channel_post" → "telegram"
//   - "bare_internal"               → ""
//   - "__weird"                     → "" (leading separator = no platform)
func TestRegistry_AllEntries_SplitsPlatform(t *testing.T) {
	reg := tools.NewRegistry()
	reg.Register(makeDef("telegram__send_channel_post"), "", nil, domain.ToolFloorManual, []string{"text"})
	reg.Register(makeDef("bare_internal"), "", nil, domain.ToolFloorAuto, nil)
	reg.Register(makeDef("__weird"), "", nil, domain.ToolFloorForbidden, nil)

	byName := make(map[string]tools.RegistryEntry)
	for _, e := range reg.AllEntries() {
		byName[e.Name] = e
	}
	assert.Len(t, byName, 3)
	assert.Equal(t, "telegram", byName["telegram__send_channel_post"].Platform)
	assert.Equal(t, domain.ToolFloorManual, byName["telegram__send_channel_post"].Floor)
	assert.ElementsMatch(t, []string{"text"}, byName["telegram__send_channel_post"].EditableFields)

	assert.Equal(t, "", byName["bare_internal"].Platform)
	assert.Equal(t, domain.ToolFloorAuto, byName["bare_internal"].Floor)

	assert.Equal(t, "", byName["__weird"].Platform)
	assert.Equal(t, domain.ToolFloorForbidden, byName["__weird"].Floor)
}

// TestRegistry_AllEntries_EditableFieldsCopy guards the same defensive-copy
// invariant at the snapshot layer — a caller must not be able to widen the
// registered allowlist by mutating the slice they received from AllEntries().
func TestRegistry_AllEntries_EditableFieldsCopy(t *testing.T) {
	reg := tools.NewRegistry()
	reg.Register(makeDef("telegram__send_channel_post"), "", nil, domain.ToolFloorManual, []string{"text"})

	entries := reg.AllEntries()
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	entries[0].EditableFields[0] = "tampered"

	fresh := reg.AllEntries()
	assert.Equal(t, "text", fresh[0].EditableFields[0])
}
