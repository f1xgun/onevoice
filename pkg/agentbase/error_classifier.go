// Package agentbase provides shared building blocks for the platform agents
// (services/agent-telegram, services/agent-vk, services/agent-yandex-business,
// services/agent-google-business). It extracts code that was duplicated 4×
// across those agents — token resolution, HITL dedupe dispatch, and
// platform-permanent error classification — into one place.
//
// The package is interface-first: TokenResolver, Dispatcher, and ErrorClassifier
// expose minimal surfaces with default implementations behind New*() constructors.
// Per phase 19 SPEC risk #5, the interfaces extract only what the existing 4×
// duplications already do — no speculative methods.
package agentbase

// ErrorClassifier wraps platform-permanent errors so the agent loop in pkg/a2a
// (and the orchestrator's NATSExecutor) does not retry them. Each agent supplies
// its own implementation; the platform-specific keyword logic lives in the agent
// package, not here. Phase 19-RESEARCH §5c documents why a "default" classifier
// with a hardcoded keyword list is an anti-pattern: the four platforms' permanent
// error markers (HTTP status strings, VK error codes, ErrSessionExpired sentinel,
// Google API status strings) are too different to share.
type ErrorClassifier interface {
	// Classify returns err unchanged if it is transient/retryable, or wrapped
	// in a non-retryable error type (typically *a2a.NonRetryableError) if it
	// represents a permanent platform failure. Returning nil when err is nil
	// is required so callers can chain Classify before any nil-check.
	Classify(err error) error
}

// FuncClassifier adapts a free function into the ErrorClassifier interface,
// for the simple per-agent string-match closures (classifyTelegramError,
// classifyVKError, classifyYandexError, classifyGBPError). Wiring example:
//
//	dispatcher := agentbase.NewDispatcher(dedupe, agentbase.FuncClassifier(classifyTelegramError))
//
// A nil FuncClassifier is treated as the identity classifier — useful for tests
// that don't care about classification.
type FuncClassifier func(error) error

// Classify implements ErrorClassifier by invoking the underlying function. When
// the receiver is nil, err is returned unchanged.
func (f FuncClassifier) Classify(err error) error {
	if f == nil {
		return err
	}
	return f(err)
}

// Compile-time check: FuncClassifier satisfies ErrorClassifier.
var _ ErrorClassifier = FuncClassifier(nil)
