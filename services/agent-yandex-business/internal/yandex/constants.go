package yandex

import "time"

// External Yandex endpoints — pinned here so DNS / path moves are tracked
// in one place. spravBaseURL composes yandexSpravEditPathFmt with a
// permalink at runtime.
const (
	yandexBusinessBaseURL   = "https://business.yandex.ru"
	yandexSpravEditPathFmt  = "https://yandex.ru/sprav/%s/p/edit"
	yandexSpravCompaniesURL = "https://yandex.ru/sprav/companies/?no_redirect=1"
	yandexPassportAuthURL   = "https://passport.yandex.ru/auth/welcome?retpath=https%3A%2F%2Fbusiness.yandex.ru"

	// Cookie hosts for context.Cookies(...). Reused as identical literals
	// inside passport-session-exchange and cookie introspection.
	yandexPassportCookieHost = "https://passport.yandex.ru"
	yandexCookieHost         = "https://yandex.ru"
)

// humanDelay range — [1000, 4000)ms — picked to mimic a human pacing
// through the Yandex.Business UI without tripping anti-bot heuristics.
const (
	humanDelayMinMs   = 1000
	humanDelayRangeMs = 3000
)

// Playwright timeouts (milliseconds). Yandex.Business is a client-side
// SPA whose listings and forms hydrate after the page is "loaded", so
// every selector wait gets a generous budget.
const (
	pageNavTimeoutMs       = 30000 // initial page navigation
	pageHydrateTimeoutMs   = 20000 // post-navigation client-side render
	tabSwitchTimeoutMs     = 15000 // switching between sub-tabs (reviews/posts/photos)
	listItemTimeoutMs      = 10000 // primary list element appearing (review card, post textarea)
	primaryActionTimeoutMs = 5000  // save button, primary actions
	uiPollTimeoutMs        = 3000  // secondary UI polling (input value reads, status checks)
	clickAwayTimeoutMs     = 2000  // dismiss-click on body / h1 to blur input
)

// defaultUserAgent for the shared Chromium context. Pinned to a recent
// stable Chrome on Windows; Yandex.Business serves a downgraded UI to
// some bot user agents that trips our selectors.
const defaultUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36"

// maxBatchLimit caps the per-call batch size for paginated read paths.
// Yandex.Business doesn't document a hard cap, but 50 keeps a single
// scroll-collect cycle within the agent's per-tool budget.
const maxBatchLimit = 50

// Playwright keyboard typing delays (milliseconds). The default is what we
// use for free-text fields; the fast variant is for already-validated text
// (e.g. paste-of-known-clean string into a fresh input).
const (
	keyboardDelayDefaultMs = 30
	keyboardDelayFastMs    = 20
)

// dialogSettleDelay is the post-action sleep used to let a dialog/popup
// finish its enter animation before we read its DOM. Empirically tuned —
// 500 ms rides above Yandex.Business's worst-case animation budget.
const dialogSettleDelay = 500 * time.Millisecond

// tmpFileMode is the standard "owner-only read/write" permission used when
// staging downloaded media to disk before re-uploading to Yandex.
const tmpFileMode = 0o600
