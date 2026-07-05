package telegram

import (
	"time"

	"github.com/f1xgun/onevoice/pkg/domain"
)

// Network retry policy for transient TLS/connection errors. api.telegram.org
// drops a non-trivial fraction of TLS handshakes on some networks; retrying
// pre-response failures is safe (no message has been processed yet).
const (
	defaultBotRetryAttempts = 3
	defaultBotRetryDelay    = 500 * time.Millisecond
)

// Review-fetch defaults applied when callers omit a limit. Sourced from the
// shared per-platform defaults so a change touches one file, not four.
const defaultReviewLimit = domain.TelegramReviewLimitDefault

// tokenRedactionMarker substitutes for the bot token in error messages
// to prevent the credential from leaking through Go's net/url default
// Error() output.
const tokenRedactionMarker = "***REDACTED_BOT_TOKEN***" //nolint:gosec // G101: this is the redaction marker substituted FOR the credential, not a credential itself

// allowedUpdateTypes restricts GetUpdates to direct messages and edits.
// Channel posts ("channel_post"/"edited_channel_post") are admin-authored
// content (operator's own announcements) and would surface as false-positive
// pending reviews; customer comments arrive as plain "message" updates in
// linked discussion groups instead.
var allowedUpdateTypes = []string{"message", "edited_message"}

// callbackUpdateTypes restricts the approval-callback long-poll to
// callback_query updates only. It is deliberately DISJOINT from
// allowedUpdateTypes so the review poll never pulls callback_query (which would
// let an inline-button tap surface as a false-positive review) and this poll
// never pulls plain messages (which belong to the review/comment plane).
var callbackUpdateTypes = []string{"callback_query"}

// callbackPollTimeout is the Telegram long-poll timeout (seconds) for the
// approval-callback poller. A positive value makes GetUpdates block server-side
// until an update arrives or the window elapses, so the loop does not busy-spin;
// it stays under the botAPITimeout HTTP deadline so the round-trip cannot wedge.
const callbackPollTimeout = 25

// startUpdateTypes restricts the /start owner-link handshake long-poll to
// message updates (the plane a "/start <token>" DM arrives on). It OVERLAPS
// allowedUpdateTypes because a review comment also arrives as "message"; the
// on-demand review poll (GetReviews) and this continuous /start poll therefore
// share the message plane. To avoid consuming a review comment, PollStart
// advances the getUpdates offset ONLY past a contiguous prefix of handled
// /start-command updates and STOPS at the first non-/start message, leaving that
// message (and everything after it) unconfirmed for GetReviews. A /start message
// re-delivered before its offset advances is deduped downstream by the token's
// single-use consume, so at-least-once delivery here is safe.
var startUpdateTypes = []string{"message"}

// startPollTimeout mirrors callbackPollTimeout: a bounded long-poll held below
// the botAPITimeout HTTP deadline so the round-trip cannot wedge.
const startPollTimeout = 25

// startCommandPrefix is the exact command token that opens a deep-link session.
// Telegram delivers "?start=<token>" as the message text "/start <token>".
const startCommandPrefix = "/start"
