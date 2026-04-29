package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/f1xgun/onevoice/pkg/llm"
)

type fakeChatter struct {
	resp *llm.ChatResponse
	err  error

	gotReq llm.ChatRequest
}

func (f *fakeChatter) Chat(_ context.Context, req llm.ChatRequest) (*llm.ChatResponse, error) {
	f.gotReq = req
	return f.resp, f.err
}

func postBody(t *testing.T, body any) *http.Request {
	t.Helper()
	buf, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return httptest.NewRequest(http.MethodPost, "/internal/draft-reply", bytes.NewReader(buf))
}

func TestDraftReplyHandler_Happy(t *testing.T) {
	chatter := &fakeChatter{
		resp: &llm.ChatResponse{Content: "  Спасибо за отзыв!  ", Provider: "openrouter"},
	}
	h := NewDraftReplyHandler(chatter, "openai/gpt-4o-mini")

	req := postBody(t, DraftReplyRequest{
		BusinessID:          "b1",
		BusinessName:        "Кофейня",
		BusinessCategory:    "Кафе",
		BusinessDescription: "Уютное место",
		Platform:            "yandex_business",
		ReviewText:          "Отличный кофе, советую!",
		Rating:              5,
		Examples: []DraftReplyExample{
			{ReviewText: "Хорошо", ReplyText: "Спасибо!", Rating: 5},
		},
	})
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rr.Code, rr.Body.String())
	}
	var got DraftReplyResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.DraftReply != "Спасибо за отзыв!" {
		t.Errorf("DraftReply trim mismatch: %q", got.DraftReply)
	}
	if got.Provider != "openrouter" {
		t.Errorf("Provider = %q, want openrouter", got.Provider)
	}

	// Sanity: prompt has system + (1 example × 2) + user = 4 messages.
	if got := len(chatter.gotReq.Messages); got != 4 {
		t.Fatalf("messages = %d, want 4", got)
	}
	if chatter.gotReq.Messages[0].Role != "system" {
		t.Errorf("first message role = %q, want system", chatter.gotReq.Messages[0].Role)
	}
	if !strings.Contains(chatter.gotReq.Messages[0].Content, "Кофейня") {
		t.Errorf("system prompt missing business name: %q", chatter.gotReq.Messages[0].Content)
	}
	if chatter.gotReq.Messages[1].Role != "user" || chatter.gotReq.Messages[2].Role != "assistant" {
		t.Errorf("few-shot pairing wrong: %+v", chatter.gotReq.Messages[1:3])
	}
	if chatter.gotReq.Messages[3].Content == "" || !strings.Contains(chatter.gotReq.Messages[3].Content, "Отличный кофе") {
		t.Errorf("final user message missing review text: %q", chatter.gotReq.Messages[3].Content)
	}
}

func TestDraftReplyHandler_RejectsEmptyReview(t *testing.T) {
	h := NewDraftReplyHandler(&fakeChatter{}, "m")
	req := postBody(t, DraftReplyRequest{ReviewText: "   "})
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rr.Code)
	}
}

func TestDraftReplyHandler_RejectsNonPOST(t *testing.T) {
	h := NewDraftReplyHandler(&fakeChatter{}, "m")
	req := httptest.NewRequest(http.MethodGet, "/internal/draft-reply", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", rr.Code)
	}
}

func TestDraftReplyHandler_LLMRateLimit(t *testing.T) {
	chatter := &fakeChatter{err: llm.ErrRateLimitExceeded}
	h := NewDraftReplyHandler(chatter, "m")
	req := postBody(t, DraftReplyRequest{ReviewText: "x"})
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusTooManyRequests {
		t.Errorf("status = %d, want 429", rr.Code)
	}
}

func TestDraftReplyHandler_LLMGenericError(t *testing.T) {
	chatter := &fakeChatter{err: errors.New("provider exploded")}
	h := NewDraftReplyHandler(chatter, "m")
	req := postBody(t, DraftReplyRequest{ReviewText: "x"})
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadGateway {
		t.Errorf("status = %d, want 502", rr.Code)
	}
}

func TestBuildDraftReplyPrompt_DropsEmptyExamples(t *testing.T) {
	msgs := buildDraftReplyPrompt(DraftReplyRequest{
		BusinessName: "X",
		ReviewText:   "review",
		Examples: []DraftReplyExample{
			{ReviewText: "valid-r", ReplyText: "valid-a"},
			{ReviewText: "", ReplyText: "no-text"},
			{ReviewText: "no-reply", ReplyText: ""},
			{ReviewText: "  ", ReplyText: "whitespace-only"},
		},
	})
	// system + 1 valid pair (×2) + final user = 4 messages
	if len(msgs) != 4 {
		t.Errorf("messages = %d, want 4: %+v", len(msgs), msgs)
	}
}

func TestFormatExampleReview_RatingPrefix(t *testing.T) {
	if got := formatExampleReview("hi", 4); !strings.HasPrefix(got, "Отзыв (4/5)") {
		t.Errorf("rating prefix missing: %q", got)
	}
	if got := formatExampleReview("hi", 0); !strings.HasPrefix(got, "Отзыв:") {
		t.Errorf("zero rating should drop /5: %q", got)
	}
}

// Smoke: end-to-end wire format match between the api drafter request and
// the orchestrator handler. If field names ever diverge this test fails.
func TestApiDrafterWireCompatibility(t *testing.T) {
	// The api side sends a struct with these JSON field names. Match our
	// handler's DraftReplyRequest by round-tripping through JSON.
	apiSide := map[string]any{
		"businessId":          "b",
		"businessName":        "n",
		"businessCategory":    "c",
		"businessDescription": "d",
		"platform":            "p",
		"reviewText":          "rt",
		"rating":              5,
		"authorName":          "a",
		"examples": []map[string]any{
			{"reviewText": "x", "replyText": "y", "rating": 4},
		},
	}
	buf, _ := json.Marshal(apiSide)
	var got DraftReplyRequest
	if err := json.Unmarshal(buf, &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.BusinessName != "n" || got.ReviewText != "rt" || got.Rating != 5 {
		t.Errorf("field mapping drift: %+v", got)
	}
	if len(got.Examples) != 1 || got.Examples[0].ReplyText != "y" {
		t.Errorf("example mapping drift: %+v", got.Examples)
	}
}

// Light reader so we don't get unused-import warnings on io if future tests
// shrink. Kept inline to be removable in one line if it ever becomes noise.
var _ = io.EOF
var _ = bytes.NewReader([]byte{})
