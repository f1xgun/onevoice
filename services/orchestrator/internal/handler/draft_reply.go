package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"github.com/f1xgun/onevoice/pkg/llm"
)

// LLM tuning for the review-draft handler. 400 tokens caps a draft reply at
// roughly 250 Russian words — long enough to cover thank-you / apology +
// concrete next step, short enough to keep tokens cheap. Temperature 0.4
// trims the boilerplate-heavy outputs of T=0.7 without sounding robotic.
const (
	draftReplyMaxTokens   = 400
	draftReplyTemperature = 0.4
)

// DraftChatter is the narrow LLM surface the draft handler needs. *llm.Router
// satisfies it; tests can pass a fake.
type DraftChatter interface {
	Chat(ctx context.Context, req llm.ChatRequest) (*llm.ChatResponse, error)
}

// DraftReplyHandler exposes POST /internal/draft-reply. The API service's
// review_drafter.go calls this with the review text, business context, and a
// short list of past (review_text, reply_text) examples; the orchestrator
// builds the prompt, calls the LLM router, and returns the generated draft.
//
// Path convention follows /internal/* (see internal_tools.go) — cluster-only.
type DraftReplyHandler struct {
	chatter DraftChatter
	model   string
}

// NewDraftReplyHandler constructs the handler. chatter is *llm.Router in
// production and a fake in tests; model must match a registered model.
func NewDraftReplyHandler(chatter DraftChatter, model string) *DraftReplyHandler {
	return &DraftReplyHandler{chatter: chatter, model: model}
}

// DraftReplyExample is one (review → owner reply) pair shown to the LLM as
// few-shot context. Caller-provided; the handler does not fetch examples.
type DraftReplyExample struct {
	ReviewText string `json:"reviewText"`
	ReplyText  string `json:"replyText"`
	Rating     int    `json:"rating,omitempty"`
}

// DraftReplyRequest is the body of POST /internal/draft-reply.
type DraftReplyRequest struct {
	BusinessID          string              `json:"businessId"`
	BusinessName        string              `json:"businessName"`
	BusinessCategory    string              `json:"businessCategory,omitempty"`
	BusinessDescription string              `json:"businessDescription,omitempty"`
	Platform            string              `json:"platform"`
	ReviewText          string              `json:"reviewText"`
	Rating              int                 `json:"rating"`
	AuthorName          string              `json:"authorName,omitempty"`
	Examples            []DraftReplyExample `json:"examples,omitempty"`
}

// DraftReplyResponse is the body returned by POST /internal/draft-reply.
type DraftReplyResponse struct {
	DraftReply string `json:"draftReply"`
	Provider   string `json:"provider,omitempty"`
	Model      string `json:"model,omitempty"`
}

// ServeHTTP implements net/http.Handler.
func (h *DraftReplyHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req DraftReplyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body: "+err.Error(), http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(req.ReviewText) == "" {
		http.Error(w, "reviewText is required", http.StatusBadRequest)
		return
	}

	messages := buildDraftReplyPrompt(req)

	chatReq := llm.ChatRequest{
		// uuid.Nil → system-level call (skips per-user rate limiting in router).
		// Billing still records the call against UserID=nil, which is fine for
		// platform-side draft generation.
		UserID:      uuid.Nil,
		Model:       h.model,
		Messages:    messages,
		MaxTokens:   draftReplyMaxTokens,
		Temperature: draftReplyTemperature,
		RequestID:   "draft-reply-" + req.BusinessID,
	}

	resp, err := h.chatter.Chat(r.Context(), chatReq)
	if err != nil {
		slog.Error("draft-reply: LLM call failed",
			"business_id", req.BusinessID,
			"platform", req.Platform,
			"error", err,
		)
		status := http.StatusBadGateway
		if errors.Is(err, llm.ErrRateLimitExceeded) {
			status = http.StatusTooManyRequests
		}
		http.Error(w, fmt.Sprintf("llm error: %v", err), status)
		return
	}

	out := DraftReplyResponse{
		DraftReply: strings.TrimSpace(resp.Content),
		Provider:   resp.Provider,
		Model:      h.model,
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(out)
}

// buildDraftReplyPrompt composes the [system, few-shot..., user] message list.
// Few-shot pairs are encoded as alternating user/assistant turns so the model
// learns the owner's tone from the actual replies they wrote, not from a
// description of them.
func buildDraftReplyPrompt(req DraftReplyRequest) []llm.Message {
	var sys strings.Builder
	sys.WriteString("Ты — ассистент, который пишет короткие ответы на отзывы клиентов от лица владельца бизнеса. ")
	sys.WriteString("Сохраняй тон и манеру ответов, которые видны в примерах ниже. ")
	sys.WriteString("Не придумывай факты, не давай скидок и обещаний, которых не было в примерах. ")
	sys.WriteString("Не используй приветствия типа 'Здравствуйте' если в примерах их нет. ")
	sys.WriteString("Отвечай только текстом ответа, без префиксов вроде 'Ответ:' или кавычек.\n\n")

	if req.BusinessName != "" {
		sys.WriteString("Бизнес: " + req.BusinessName)
		if req.BusinessCategory != "" {
			sys.WriteString(" (" + req.BusinessCategory + ")")
		}
		sys.WriteString(".\n")
	}
	if req.BusinessDescription != "" {
		sys.WriteString("Описание: " + req.BusinessDescription + "\n")
	}
	if req.Platform != "" {
		sys.WriteString("Платформа отзыва: " + req.Platform + ".\n")
	}

	msgs := make([]llm.Message, 0, 2+2*len(req.Examples))
	msgs = append(msgs, llm.Message{Role: "system", Content: sys.String()})

	for _, ex := range req.Examples {
		if strings.TrimSpace(ex.ReviewText) == "" || strings.TrimSpace(ex.ReplyText) == "" {
			continue
		}
		msgs = append(msgs, llm.Message{Role: "user", Content: formatExampleReview(ex.ReviewText, ex.Rating)})
		msgs = append(msgs, llm.Message{Role: "assistant", Content: ex.ReplyText})
	}

	msgs = append(msgs, llm.Message{Role: "user", Content: formatExampleReview(req.ReviewText, req.Rating)})
	return msgs
}

// formatExampleReview wraps a review text with its rating so the model can
// see the relationship between sentiment (stars) and the owner's reply style.
func formatExampleReview(text string, rating int) string {
	if rating > 0 {
		return fmt.Sprintf("Отзыв (%d/5): %s", rating, text)
	}
	return "Отзыв: " + text
}
