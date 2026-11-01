package service

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/google/uuid"

	"github.com/f1xgun/onevoice/pkg/domain"
)

const decisionKindApproval = "approval_batch"

// DecisionQueue is a read projection. Mutations keep their existing review and HITL contracts.
type DecisionQueue struct {
	Items     []DecisionItem    `json:"items"`
	Total     int               `json:"total"`
	Automatic []domain.AuditLog `json:"automatic"`
}

type DecisionItem struct {
	ID             string            `json:"id"`
	Kind           string            `json:"kind"`
	Platform       string            `json:"platform,omitempty"`
	CreatedAt      time.Time         `json:"createdAt"`
	Review         *domain.Review    `json:"review,omitempty"`
	ConversationID string            `json:"conversationId,omitempty"`
	CallCount      int               `json:"callCount,omitempty"`
	Drift          *domain.SyncState `json:"drift,omitempty"`
}

type DecisionAccess struct{ Content, Integrations, Audit bool }

type DecisionQueueService struct {
	reviews      domain.ReviewRepository
	pending      domain.BusinessPendingToolCallReader
	integrations domain.IntegrationRepository
	sync         domain.SyncStateRepository
	audit        domain.AuditLogRepository
}

func NewDecisionQueueService(reviews domain.ReviewRepository, pending domain.BusinessPendingToolCallReader, integrations domain.IntegrationRepository, sync domain.SyncStateRepository, audit domain.AuditLogRepository) *DecisionQueueService {
	return &DecisionQueueService{reviews: reviews, pending: pending, integrations: integrations, sync: sync, audit: audit}
}

func (s *DecisionQueueService) List(ctx context.Context, businessID, userID uuid.UUID, access DecisionAccess) (DecisionQueue, error) {
	out := DecisionQueue{Items: []DecisionItem{}, Automatic: []domain.AuditLog{}}
	if access.Content {
		reviews, total, err := s.reviews.ListByBusinessID(ctx, businessID.String(), domain.ReviewFilter{ReplyStatus: domain.ReviewReplyStatusPending, Limit: 100})
		if err != nil {
			return out, fmt.Errorf("decision reviews: %w", err)
		}
		failed, failedTotal, err := s.reviews.ListByBusinessID(ctx, businessID.String(), domain.ReviewFilter{ReplyStatus: domain.ReviewReplyStatusError, Limit: 100})
		if err != nil {
			return out, fmt.Errorf("decision failed replies: %w", err)
		}
		reviews = append(failed, reviews...)
		if len(reviews) > 100 {
			reviews = reviews[:100]
		}
		out.Total += total + failedTotal
		for i := range reviews {
			r := reviews[i]
			out.Items = append(out.Items, DecisionItem{ID: "review:" + r.ID, Kind: "review", Platform: r.Platform, CreatedAt: r.CreatedAt, Review: &r})
		}
		batches, err := s.pending.ListPendingByBusiness(ctx, businessID.String(), userID.String())
		if err != nil {
			return out, fmt.Errorf("decision approvals: %w", err)
		}
		for _, b := range batches {
			if b.UserID != userID.String() || b.Status != domain.ReviewReplyStatusPending || len(b.Calls) == 0 || (!b.ExpiresAt.IsZero() && !b.ExpiresAt.After(time.Now())) {
				continue
			}
			out.Items = append(out.Items, DecisionItem{ID: "approval:" + b.ID, Kind: decisionKindApproval, CreatedAt: b.CreatedAt, ConversationID: b.ConversationID, CallCount: len(b.Calls)})
			out.Total++
		}
	}
	if access.Integrations {
		integrations, err := s.integrations.ListByBusinessID(ctx, businessID)
		if err != nil {
			return out, fmt.Errorf("decision connections: %w", err)
		}
		for _, i := range integrations {
			health, _ := i.Metadata["connection_health"].(map[string]interface{})
			if i.Status != "token_expired" && health["status"] != "broken" {
				continue
			}
			out.Items = append(out.Items, DecisionItem{ID: "connection:" + i.ID.String(), Kind: "connection", Platform: i.Platform, CreatedAt: i.CreatedAt})
			out.Total++
		}
		drift, err := s.sync.ListByBusinessID(ctx, businessID)
		if err != nil {
			return out, fmt.Errorf("decision drift: %w", err)
		}
		for i := range drift {
			d := drift[i]
			if !d.DriftDetected || d.LastCheckedAt == nil {
				continue
			}
			out.Items = append(out.Items, DecisionItem{ID: "drift:" + d.ID.String(), Kind: "drift", Platform: d.Platform, CreatedAt: d.UpdatedAt, Drift: &d})
			out.Total++
		}
	}
	if access.Audit {
		logs, err := s.audit.ListByBusiness(ctx, businessID, domain.AuditLogFilter{Action: "review.auto_replied", Limit: 10})
		if err != nil {
			return out, fmt.Errorf("decision automatic receipts: %w", err)
		}
		// Defense in depth: never relabel a manual audit entry as automatic.
		for _, log := range logs {
			if log.Action == "review.auto_replied" {
				out.Automatic = append(out.Automatic, log)
			}
		}
	}
	sort.SliceStable(out.Items, func(i, j int) bool { return out.Items[i].CreatedAt.Before(out.Items[j].CreatedAt) })
	return out, nil
}
