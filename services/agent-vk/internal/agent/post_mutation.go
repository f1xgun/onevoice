package agent

import (
	"context"
	"fmt"
	"strconv"

	"github.com/f1xgun/onevoice/pkg/a2a"
	"github.com/f1xgun/onevoice/pkg/agentbase"
	"github.com/f1xgun/onevoice/pkg/tools"
)

func (h *Handler) mutatePost(ctx context.Context, req a2a.ToolRequest) (*a2a.ToolResponse, error) {
	supplied, _ := req.Args["group_id"].(string)
	groupID := ensureNegativeGroupID(supplied)
	owner, ownerErr := strconv.ParseInt(groupID, 10, 64)
	id, err := a2a.GetIntParam(req.Args, "post_id", 0)
	number, isNumber := req.Args["post_id"].(float64)
	if ownerErr != nil || owner >= 0 || supplied == "" || err != nil || id <= 0 || (isNumber && number != float64(id)) {
		return nil, a2a.NewNonRetryableError(fmt.Errorf("vk: explicit community group_id and positive integer post_id are required"))
	}
	text, textOK := req.Args["text"].(string)
	if req.Tool == tools.VKEditPost && !textOK {
		return nil, a2a.NewNonRetryableError(fmt.Errorf("vk: text is required"))
	}
	info, err := agentbase.FetchToken(ctx, h.tokens, req.BusinessID, a2a.AgentVK, supplied, req.Tool)
	if err != nil {
		return nil, err
	}
	if groupID != ensureNegativeGroupID(info.ExternalID) {
		return nil, a2a.NewNonRetryableError(fmt.Errorf("vk: group_id does not match the connected community"))
	}
	if info.UserToken == "" {
		return nil, a2a.NewCodedError("integration_token_invalid", a2a.NewNonRetryableError(fmt.Errorf("vk: editing and deleting posts require the connected administrator's user token")))
	}
	client := h.clientFactory(info.UserToken)
	status := "deleted"
	if req.Tool == tools.VKEditPost {
		err = client.EditPost(groupID, id, text)
		status = "edited"
	} else {
		err = client.DeletePost(groupID, id)
	}
	if err != nil {
		return nil, fmt.Errorf("vk: mutate post: %w", classifyVKError(err))
	}
	result := publishedPostResult(groupID, int64(id))
	result["status"] = status
	return a2a.OK(req, result), nil
}
