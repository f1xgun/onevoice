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

	"golang.org/x/text/language"

	"github.com/f1xgun/onevoice/pkg/i18n"
	"github.com/f1xgun/onevoice/pkg/llm"
)

// i18nWithEnglish is a thin test helper: attach language.English to ctx so
// handler-level tests can drive the locale path without spinning up the full
// middleware chain.
func i18nWithEnglish(ctx context.Context) context.Context {
	return i18n.WithLocale(ctx, language.English)
}

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
	req := httptest.NewRequest(http.MethodGet, "/internal/draft-reply", http.NoBody)
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
	}, language.Russian)
	// system + 1 valid pair (×2) + final user = 4 messages
	if len(msgs) != 4 {
		t.Errorf("messages = %d, want 4: %+v", len(msgs), msgs)
	}
}

func TestFormatExampleReview_RatingPrefix(t *testing.T) {
	if got := formatExampleReview("hi", 4, language.Russian); !strings.HasPrefix(got, "Отзыв (4/5)") {
		t.Errorf("rating prefix missing: %q", got)
	}
	if got := formatExampleReview("hi", 0, language.Russian); !strings.HasPrefix(got, "Отзыв:") {
		t.Errorf("zero rating should drop /5: %q", got)
	}
}

// --- locale-aware draft-reply prompt ---

func TestFormatExampleReview_EnglishLocale(t *testing.T) {
	if got := formatExampleReview("hi", 4, language.English); !strings.HasPrefix(got, "Review (4/5)") {
		t.Errorf("EN rating prefix missing: %q", got)
	}
	if got := formatExampleReview("hi", 0, language.English); !strings.HasPrefix(got, "Review:") {
		t.Errorf("EN zero-rating prefix missing: %q", got)
	}
}

func TestBuildDraftReplyPrompt_EnglishLocale_SystemAndFraming(t *testing.T) {
	msgs := buildDraftReplyPrompt(DraftReplyRequest{
		BusinessName:        "Acme",
		BusinessCategory:    "café",
		BusinessDescription: "Cozy café",
		Platform:            "google_business",
		ReviewText:          "great place",
		Rating:              5,
		Examples: []DraftReplyExample{
			{ReviewText: "ok", ReplyText: "thanks", Rating: 4},
		},
	}, language.English)

	// system + 1 valid pair (×2) + final user = 4 messages
	if len(msgs) != 4 {
		t.Fatalf("messages = %d, want 4", len(msgs))
	}
	sys := msgs[0].Content
	// Sample lock-in: EN-only phrases that must appear.
	for _, want := range []string{
		"You are an assistant",
		"Preserve the tone",
		"Do not invent facts",
		"Business: Acme (café)",
		"Description: Cozy café",
		"Review platform: google_business",
	} {
		if !strings.Contains(sys, want) {
			t.Errorf("EN system prompt missing %q\nfull:\n%s", want, sys)
		}
	}
	for _, leak := range []string{
		"Ты — ассистент",
		"Бизнес:",
		"Платформа отзыва:",
		"Отзыв",
	} {
		if strings.Contains(sys, leak) {
			t.Errorf("EN system prompt leaked RU: %q", leak)
		}
	}

	// Final user message uses EN framing.
	last := msgs[len(msgs)-1].Content
	if !strings.HasPrefix(last, "Review (5/5):") {
		t.Errorf("EN final user framing missing: %q", last)
	}
}

func TestDraftReplyHandler_EnglishLocale_E2E(t *testing.T) {
	// Drive the full handler with a request ctx carrying language.English to
	// verify the locale → system prompt → LLM call wiring end-to-end. The
	// LLM (fakeChatter) records the prompt it received; we assert on English
	// framing in the captured request.
	chatter := &fakeChatter{
		resp: &llm.ChatResponse{Content: "Thanks for the review!", Provider: "openrouter"},
	}
	h := NewDraftReplyHandler(chatter, "openai/gpt-4o-mini")

	body, _ := json.Marshal(DraftReplyRequest{
		BusinessID:   "b1",
		BusinessName: "Cozy Café",
		ReviewText:   "Great coffee!",
		Rating:       5,
	})
	req := httptest.NewRequest(http.MethodPost, "/internal/draft-reply", bytes.NewReader(body))
	req = req.WithContext(i18nWithEnglish(req.Context()))
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d", rr.Code)
	}
	if got := chatter.gotReq.Messages[0].Content; !strings.Contains(got, "You are an assistant") {
		t.Errorf("system prompt should be English, got: %q", got)
	}
	final := chatter.gotReq.Messages[len(chatter.gotReq.Messages)-1].Content
	if !strings.HasPrefix(final, "Review (5/5):") {
		t.Errorf("final user message should use EN framing: %q", final)
	}
}

func TestBuildDraftReplyPrompt_RussianLocale_PreservesLegacyShape(t *testing.T) {
	// Byte-compat regression: a RU-locale draft must contain every legacy
	// substring tests + production rely on.
	msgs := buildDraftReplyPrompt(DraftReplyRequest{
		BusinessName: "Кофейня",
		ReviewText:   "хороший кофе",
		Rating:       5,
	}, language.Russian)
	sys := msgs[0].Content
	for _, want := range []string{
		"Ты — ассистент",
		"Сохраняй тон",
		"Не придумывай факты",
		"Бизнес: Кофейня",
	} {
		if !strings.Contains(sys, want) {
			t.Errorf("RU system prompt missing %q\nfull:\n%s", want, sys)
		}
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

// TestDraftReply_PassesBusinessIDFromRequest pins business-attribution for
// the draft-reply path: the handler MUST parse req.BusinessID into
// ChatRequest.BusinessID so the resulting cost row attributes to the correct
// business.
func TestDraftReply_PassesBusinessIDFromRequest(t *testing.T) {
	bizID := "11111111-2222-3333-4444-555555555555"
	chatter := &fakeChatter{
		resp: &llm.ChatResponse{Content: "Thanks!", Provider: "openrouter"},
	}
	h := NewDraftReplyHandler(chatter, "openai/gpt-4o-mini")

	req := postBody(t, DraftReplyRequest{
		BusinessID: bizID,
		ReviewText: "Great service",
		Rating:     5,
	})
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rr.Code, rr.Body.String())
	}
	got := chatter.gotReq.BusinessID.String()
	if got != bizID {
		t.Errorf("ChatRequest.BusinessID = %q, want %q", got, bizID)
	}
}

// TestDraftReply_MalformedBusinessID_DegradesToNil — fail-closed: non-UUID
// business_id (e.g. legacy "b1" sentinel) does NOT crash the handler and
// BusinessID lands as uuid.Nil so the router skips billing instead of writing
// a corrupt row.
func TestDraftReply_MalformedBusinessID_DegradesToNil(t *testing.T) {
	chatter := &fakeChatter{
		resp: &llm.ChatResponse{Content: "ok", Provider: "openrouter"},
	}
	h := NewDraftReplyHandler(chatter, "openai/gpt-4o-mini")

	req := postBody(t, DraftReplyRequest{
		BusinessID: "not-a-uuid",
		ReviewText: "x",
		Rating:     5,
	})
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rr.Code, rr.Body.String())
	}
	if chatter.gotReq.BusinessID.String() != "00000000-0000-0000-0000-000000000000" {
		t.Errorf("malformed business_id must degrade to uuid.Nil, got %q", chatter.gotReq.BusinessID)
	}
}

// Light reader so we don't get unused-import warnings on io if future tests
// shrink. Kept inline to be removable in one line if it ever becomes noise.
var _ = io.EOF
var _ = bytes.NewReader([]byte{})
