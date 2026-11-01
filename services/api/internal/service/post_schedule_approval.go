package service

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/f1xgun/onevoice/pkg/a2a"
	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/pkg/tools"
)

// ApprovedPostSchedule carries the existing resume proof over the internal mTLS route.
type ApprovedPostSchedule struct {
	BusinessID     string                 `json:"business_id"`
	ConversationID string                 `json:"conversation_id"`
	ApprovalID     string                 `json:"approval_id"`
	Tool           string                 `json:"tool"`
	Args           map[string]interface{} `json:"args"`
}

func WithPostApproval(svc PostService, pending domain.PendingToolCallRepository, conversations domain.ConversationRepository) PostService {
	if s, ok := svc.(*postService); ok {
		s.pending = pending
		s.conversations = conversations
	}
	return svc
}

type ApprovedScheduleResult struct {
	Post       *domain.Post
	Immediate  bool
	ExternalID string
}

// ScheduleApproved accepts only the exact durable approve/edit decision in this conversation.
func (s *postService) ScheduleApproved(ctx context.Context, in ApprovedPostSchedule) (*ApprovedScheduleResult, error) {
	const batchUUIDLength = 36
	if len(in.ApprovalID) <= batchUUIDLength+1 || in.ApprovalID[batchUUIDLength] != '-' || s.pending == nil || s.conversations == nil {
		return nil, domain.ErrForbidden
	}
	batchID, callID := in.ApprovalID[:batchUUIDLength], in.ApprovalID[batchUUIDLength+1:]
	if _, err := uuid.Parse(batchID); err != nil {
		return nil, domain.ErrForbidden
	}
	batch, err := s.pending.GetByBatchID(ctx, batchID)
	if err != nil {
		return nil, fmt.Errorf("load scheduling approval: %w", err)
	}
	if batch.ID != batchID || batch.BusinessID != in.BusinessID || batch.ConversationID != in.ConversationID || (batch.Status != "resuming" && batch.Status != "resolved") {
		return nil, domain.ErrForbidden
	}
	userID, err := uuid.Parse(batch.UserID)
	if err != nil || userID == uuid.Nil {
		return nil, domain.ErrForbidden
	}
	conversation, err := s.conversations.GetByID(ctx, in.ConversationID)
	if err != nil {
		return nil, fmt.Errorf("load scheduling conversation: %w", err)
	}
	if conversation.UserID != batch.UserID || conversation.BusinessID != in.BusinessID {
		return nil, domain.ErrForbidden
	}
	var approved *domain.PendingCall
	for i := range batch.Calls {
		if batch.Calls[i].CallID == callID {
			approved = &batch.Calls[i]
			break
		}
	}
	if approved == nil || approved.ToolName != in.Tool || (approved.Verdict != actionApprove && approved.Verdict != actionEdit) || (in.Tool != tools.TelegramSendChannelPost && in.Tool != tools.TelegramSendChannelPhoto) {
		return nil, domain.ErrForbidden
	}
	effective := make(map[string]interface{}, len(approved.Arguments)+len(approved.EditedArgs))
	for key, value := range approved.Arguments {
		effective[key] = value
	}
	if approved.Verdict == actionEdit {
		for key, value := range approved.EditedArgs {
			effective[key] = value
		}
	}
	expected, err := json.Marshal(effective)
	if err != nil {
		return nil, domain.ErrForbidden
	}
	actual, err := json.Marshal(in.Args)
	if err != nil || string(expected) != string(actual) {
		return nil, domain.ErrForbidden
	}
	date, typed := effective["scheduled_at"].(string)
	var at time.Time
	if date != "" {
		at, err = time.Parse(time.RFC3339, date)
	}
	if !typed || err != nil {
		return nil, fmt.Errorf("%w: scheduled_at must be RFC3339", domain.ErrPostConflict)
	}
	businessID, err := uuid.Parse(in.BusinessID)
	if err != nil {
		return nil, domain.ErrForbidden
	}
	target, _ := effective["channel_id"].(string)
	if target == "" {
		if s.integrations == nil {
			return nil, domain.ErrPostConflict
		}
		integrations, lookupErr := s.integrations.ListByBusinessAndPlatform(ctx, businessID, a2a.AgentTelegram)
		if lookupErr != nil {
			return nil, fmt.Errorf("resolve scheduled channel: %w", lookupErr)
		}
		for _, integration := range integrations {
			if integration.BusinessID == businessID && integration.Platform == a2a.AgentTelegram && integration.Status == postIntegrationActive && integration.ExternalID != "" {
				if target != "" {
					return nil, fmt.Errorf("%w: choose the Telegram channel explicitly when multiple channels are active", domain.ErrPostConflict)
				}
				target = integration.ExternalID
			}
		}
	}
	if target == "" {
		return nil, fmt.Errorf("%w: an active Telegram channel is required", domain.ErrPostConflict)
	}
	if date == "" {
		edited, present := approved.EditedArgs["scheduled_at"]
		if approved.Verdict != actionEdit || !present || edited != "" {
			return nil, domain.ErrForbidden
		}
		if s.integrations == nil {
			return nil, domain.ErrPostConflict
		}
		integration, lookupErr := s.integrations.GetByBusinessPlatformExternal(ctx, businessID, a2a.AgentTelegram, target)
		if lookupErr != nil || integration == nil || integration.BusinessID != businessID || integration.ExternalID != target || integration.Status != postIntegrationActive {
			return nil, domain.ErrForbidden
		}
		return &ApprovedScheduleResult{Immediate: true, ExternalID: target}, nil
	}
	content, _ := effective["text"].(string)
	var media []string
	if in.Tool == tools.TelegramSendChannelPhoto {
		content, _ = effective["caption"].(string)
		photo, _ := effective["photo_url"].(string)
		if photo == "" {
			return nil, domain.ErrPostConflict
		}
		media = []string{photo}
	}
	id := uuid.NewSHA1(uuid.NameSpaceOID, []byte(in.BusinessID+":"+in.ApprovalID)).String()
	post, err := s.createSchedule(ctx, businessID, target, content, media, at, id, batch.MessageID)
	if err != nil {
		return nil, err
	}
	return &ApprovedScheduleResult{Post: post}, nil
}
