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
