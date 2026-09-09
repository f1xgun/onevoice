package domain

import (
	"time"

	"github.com/google/uuid"
)

type ContentTemplateKind string

const (
	ContentTemplateKindPost        ContentTemplateKind = "post"
	ContentTemplateKindReviewReply ContentTemplateKind = "review_reply"
)

type ContentTemplate struct {
	ID           uuid.UUID           `json:"id"`
	BusinessID   uuid.UUID           `json:"businessId"`
	CreatedBy    uuid.UUID           `json:"createdBy"`
	Name         string              `json:"name"`
	Kind         ContentTemplateKind `json:"kind"`
	Body         string              `json:"body"`
	Placeholders []string            `json:"placeholders"`
	CreatedAt    time.Time           `json:"createdAt"`
	UpdatedAt    time.Time           `json:"updatedAt"`
}
