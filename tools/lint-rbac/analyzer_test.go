package main

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"
)

func TestAnalyzer_Clean(t *testing.T) {
	prev := activeAllowlist
	activeAllowlist = nil
	t.Cleanup(func() { activeAllowlist = prev })
	analysistest.Run(t, analysistest.TestData(), Analyzer, "clean")
}

func TestAnalyzer_Violation(t *testing.T) {
	prev := activeAllowlist
	activeAllowlist = nil
	t.Cleanup(func() { activeAllowlist = prev })
	analysistest.Run(t, analysistest.TestData(), Analyzer, "violation")
}

// HI-02: when both violating routes are allowlisted, the analyzer must
// suppress all diagnostics. Asserts the IsAllowed wire-up is live.
func TestAnalyzer_AllowlistSuppressesDiagnostics(t *testing.T) {
	prev := activeAllowlist
	activeAllowlist = []AllowlistEntry{
		{Route: "Get /foo", Reason: "test", Expires: "2099-12-31"},
		{Route: "Post /bar", Reason: "test", Expires: "2099-12-31"},
	}
	t.Cleanup(func() { activeAllowlist = prev })

	// Use the no-diagnostic fixture; analysistest.Run with the violation
	// package would require the fixture to also drop the `// want` comments.
	// Instead, point at a fresh subdir that mirrors violation but without
	// `// want` annotations — see testdata/src/violation_allowlisted.
	analysistest.Run(t, analysistest.TestData(), Analyzer, "violation_allowlisted")
}
