package service

import (
	"strconv"

	"github.com/f1xgun/onevoice/pkg/a2a"
	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/services/api/internal/platform"
)

// factualCommentReply is deliberately narrower than the model-draft path.
func factualCommentReply(business *domain.Business, review *domain.Review) (string, bool) {
	if review.NeedsReview || review.Rating != 0 || review.ReplyStatus != domain.ReviewReplyStatusPending || (review.Platform != a2a.AgentTelegram && review.Platform != a2a.AgentVK) {
		return "", false
	}
	reply, ok := platform.FactualBusinessReply(business, review.Text)
	if !ok || !isDraftSafeForAutoPublish(reply) {
		return "", false
	}
	tool, args, err := buildPlatformReply(review, reply)
	if err != nil {
		return "", false
	}
	if floor, explicit := business.ToolApprovals()[tool]; explicit && floor != domain.ToolFloorAuto {
		return "", false
	}
	if review.Platform == a2a.AgentTelegram {
		chat, err := strconv.ParseInt(args["chat_id"].(string), 10, 64)
		if err != nil || chat >= 0 || args["message_id"].(float64) <= 0 {
			return "", false
		}
	} else if args["post_id"].(float64) <= 0 || args["comment_id"].(float64) <= 0 {
		return "", false
	}
	return reply, true
}
