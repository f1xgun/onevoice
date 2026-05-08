package telegram

import "time"

// Network retry policy for transient TLS/connection errors. api.telegram.org
// drops a non-trivial fraction of TLS handshakes on some networks; retrying
// pre-response failures is safe (no message has been processed yet).
const (
	defaultBotRetryAttempts = 3
	defaultBotRetryDelay    = 500 * time.Millisecond
)

// Review-fetch defaults applied when callers omit a limit.
const defaultReviewLimit = 500

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
