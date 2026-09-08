// Package approvaltelemetry defines the content-free approval funnel contract.
package approvaltelemetry

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"regexp"

	"github.com/f1xgun/onevoice/pkg/tools"
)

// Event contains identifiers and closed classifications only, never tool payloads.
type Event struct {
	BatchID    string
	DraftID    string
	ApprovalID string
	Kind       string
	Source     string
	Action     string
	Outcome    string
}

// Sink records best-effort server events without affecting approval or dispatch.
type Sink interface {
	RecordApproval(ctx context.Context, events ...Event)
}

// ID hashes the existing dispatch key, not content or personal data.
func ID(key string) string {
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:])
}

var hashPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)

// ClientMetadata projects untrusted input onto the closed client schema.
func ClientMetadata(action string, metadata map[string]string) (map[string]string, bool) {
	if action != "draft_shown" && action != "approval_shown" && action != "edit_saved" {
		return nil, false
	}
	if !hashPattern.MatchString(metadata["draft_id"]) || !ValidKind(metadata["kind"]) || !ValidSource(metadata["source"]) {
		return nil, false
	}
	return map[string]string{"draft_id": metadata["draft_id"], "kind": metadata["kind"], "source": metadata["source"]}, true
}

// ValidKind restricts categories to content types covered by the funnel.
func ValidKind(kind string) bool {
	return kind == "post" || kind == "review_reply"
}

// ValidSource restricts surfaces to the two approval entry points.
func ValidSource(source string) bool {
	return source == "chat" || source == "reviews"
}

// Kind maps registered publishing tools to a bounded content category.
func Kind(tool string) string {
	switch tool {
	case tools.TelegramSendChannelPost, tools.TelegramSendChannelPhoto, tools.VKPublishPost, tools.VKPostPhoto, tools.VKSchedulePost, tools.YandexBusinessCreatePost:
		return "post"
	case tools.TelegramReplyToComment, tools.VKReplyComment, tools.YandexBusinessReplyReview, tools.GoogleBusinessReplyReview:
		return "review_reply"
	default:
		return ""
	}
}
