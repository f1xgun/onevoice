package hitlvalidation_test

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/pkg/hitlvalidation"
)

// newCaptureLogger replaces slog's default logger with one backed by a buffer
// so tests can assert the exact tool_approval_whitelist_unknown events emitted
// by ValidateApprovalSettings. Restores the previous default via t.Cleanup.
func newCaptureLogger(t *testing.T) *bytes.Buffer {
	t.Helper()
	buf := &bytes.Buffer{}
	handler := slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	prev := slog.Default()
	slog.SetDefault(slog.New(handler))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return buf
}

func TestValidateApprovalSettings_UnknownBusinessTool_LogsWarning(t *testing.T) {
	buf := newCaptureLogger(t)

	registered := map[string]struct{}{
		"telegram__send_channel_post": {},
	}
	businesses := []hitlvalidation.ApprovalSource{{
		ID: "biz-123",
		Overrides: map[string]domain.ToolFloor{
			"renamed_or_missing__tool": domain.ToolFloorManual,
		},
	}}

	n := hitlvalidation.ValidateApprovalSettings(context.Background(), registered, businesses, nil)
	if n != 1 {
		t.Fatalf("warnCount = %d, want 1", n)
	}

	logs := buf.String()
	assertContains(t, logs, "tool_approval_whitelist_unknown")
	assertContains(t, logs, "scope=business")
	assertContains(t, logs, "id=biz-123")
	assertContains(t, logs, "tool=renamed_or_missing__tool")
	assertContains(t, logs, "action=treated_as_denied")
}

func TestValidateApprovalSettings_UnknownProjectTool_LogsWarning(t *testing.T) {
	buf := newCaptureLogger(t)

	registered := map[string]struct{}{
		"telegram__send_channel_post": {},
	}
	projects := []hitlvalidation.ApprovalSource{{
		ID: "proj-abc",
		Overrides: map[string]domain.ToolFloor{
			"ghost__tool": domain.ToolFloorForbidden,
		},
	}}

	n := hitlvalidation.ValidateApprovalSettings(context.Background(), registered, nil, projects)
	if n != 1 {
		t.Fatalf("warnCount = %d, want 1", n)
	}

	logs := buf.String()
	assertContains(t, logs, "tool_approval_whitelist_unknown")
	assertContains(t, logs, "scope=project")
	assertContains(t, logs, "id=proj-abc")
	assertContains(t, logs, "tool=ghost__tool")
}

// TestValidateApprovalSettings_AllKnown_NoWarnings guards the happy path:
// every entry in every source maps to a registered tool → zero warnings,
// zero log lines at WARN level.
func TestValidateApprovalSettings_AllKnown_NoWarnings(t *testing.T) {
	buf := newCaptureLogger(t)

	registered := map[string]struct{}{
		"telegram__send_channel_post": {},
		"vk__publish_post":            {},
	}
	businesses := []hitlvalidation.ApprovalSource{{
		ID: "biz-1",
		Overrides: map[string]domain.ToolFloor{
			"telegram__send_channel_post": domain.ToolFloorManual,
		},
	}}
	projects := []hitlvalidation.ApprovalSource{{
		ID: "proj-1",
		Overrides: map[string]domain.ToolFloor{
			"vk__publish_post": domain.ToolFloorManual,
		},
	}}

	n := hitlvalidation.ValidateApprovalSettings(context.Background(), registered, businesses, projects)
	if n != 0 {
		t.Fatalf("warnCount = %d, want 0 (all tools known)", n)
	}
	if strings.Contains(buf.String(), "tool_approval_whitelist_unknown") {
		t.Fatalf("no warnings expected, got logs:\n%s", buf.String())
	}
}

// TestValidateApprovalSettings_MixedKnownUnknown_CountsUnknownOnly guards the
// aggregate count math: two known + one unknown → count=1. Exercises both
// scopes to prevent a regression where one of the two loops silently exits.
func TestValidateApprovalSettings_MixedKnownUnknown_CountsUnknownOnly(t *testing.T) {
	buf := newCaptureLogger(t)

	registered := map[string]struct{}{
		"telegram__send_channel_post": {},
		"vk__publish_post":            {},
	}
	businesses := []hitlvalidation.ApprovalSource{{
		ID: "biz-1",
		Overrides: map[string]domain.ToolFloor{
			"telegram__send_channel_post": domain.ToolFloorManual,
			"bogus_biz__tool":             domain.ToolFloorAuto,
		},
	}}
	projects := []hitlvalidation.ApprovalSource{{
		ID: "proj-1",
		Overrides: map[string]domain.ToolFloor{
			"vk__publish_post":    domain.ToolFloorManual,
			"bogus_proj__missing": domain.ToolFloorManual,
		},
	}}

	n := hitlvalidation.ValidateApprovalSettings(context.Background(), registered, businesses, projects)
	if n != 2 {
		t.Fatalf("warnCount = %d, want 2 (one unknown per scope)", n)
	}

	logs := buf.String()
	assertContains(t, logs, "bogus_biz__tool")
	assertContains(t, logs, "bogus_proj__missing")
}

// TestValidateApprovalSettings_EmptyInputs is a degenerate guard — nil + nil +
// empty registered map yields zero warnings and zero panics.
func TestValidateApprovalSettings_EmptyInputs(t *testing.T) {
	buf := newCaptureLogger(t)
	n := hitlvalidation.ValidateApprovalSettings(context.Background(), map[string]struct{}{}, nil, nil)
	if n != 0 {
		t.Fatalf("warnCount = %d, want 0", n)
	}
	if buf.Len() != 0 {
		t.Fatalf("no logs expected, got: %q", buf.String())
	}
}

// TestValidateApprovalSettings_MultipleSourcesSameScope exercises iteration
// across many ApprovalSources of the same scope (the API service produces one
// per business / per project — this test guards that none of them are
// silently skipped).
func TestValidateApprovalSettings_MultipleSourcesSameScope(t *testing.T) {
	newCaptureLogger(t)

	registered := map[string]struct{}{
		"telegram__send_channel_post": {},
	}
	businesses := []hitlvalidation.ApprovalSource{
		{ID: "biz-1", Overrides: map[string]domain.ToolFloor{"telegram__send_channel_post": domain.ToolFloorManual}},
		{ID: "biz-2", Overrides: map[string]domain.ToolFloor{"deprecated__thing": domain.ToolFloorManual}},
		{ID: "biz-3", Overrides: map[string]domain.ToolFloor{"another__ghost": domain.ToolFloorAuto}},
	}

	n := hitlvalidation.ValidateApprovalSettings(context.Background(), registered, businesses, nil)
	if n != 2 {
		t.Fatalf("warnCount = %d, want 2 (biz-2 + biz-3 reference unknown tools)", n)
	}
}

func assertContains(t *testing.T, haystack, needle string) {
	t.Helper()
	if !strings.Contains(haystack, needle) {
		t.Fatalf("log output missing %q; full logs:\n%s", needle, haystack)
	}
}
