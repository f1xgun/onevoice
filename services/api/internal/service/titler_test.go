package service

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"

	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/pkg/llm"
)

// fakeRouter implements the package-private chatCaller interface so we can
// drive the titler without spinning up a real *llm.Router. It records the
// last ChatRequest so tests can assert on the prompt body (D-14 pre-redact
// proof) and returns either the canned content or the canned error.
type fakeRouter struct {
	mu            sync.Mutex
	returnContent string
	returnErr     error
	lastReq       *llm.ChatRequest
}

func (f *fakeRouter) Chat(_ context.Context, req llm.ChatRequest) (*llm.ChatResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	reqCopy := req
	f.lastReq = &reqCopy
	if f.returnErr != nil {
		return nil, f.returnErr
	}
	return &llm.ChatResponse{Content: f.returnContent}, nil
}

// fakeConvRepo embeds domain.ConversationRepository as a NIL interface
// (W-04 resolution: nil-embedded-interface is the chosen strategy over
// implementing every method as a stub — fewer LOC, louder failure mode).
//
// Calling any method other than UpdateTitleIfPending nil-panics, which is
// intentional: the Titler is not permitted to call any other repo method,
// and a panic surfaces such misuse loudly. Only UpdateTitleIfPending is
// overridden with real behavior.
type fakeConvRepo struct {
	domain.ConversationRepository // nil — sentinel for "must not be called"
	mu                            sync.Mutex
	updateCalls                   []struct{ ID, Title string }
	updateRetErr                  error
}

func (r *fakeConvRepo) UpdateTitleIfPending(_ context.Context, id, title string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.updateCalls = append(r.updateCalls, struct{ ID, Title string }{id, title})
	return r.updateRetErr
}

// captureLogs swaps slog.Default for a TextHandler-backed *bytes.Buffer
// during the test, then restores the original logger via t.Cleanup. Returns
// the buffer so tests can negative-assert on its bytes (Landmine 6 / Pitfall
// 8 — log-shape regression test).
func captureLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	buf := &bytes.Buffer{}
	prevLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prevLogger) })
	return buf
}

// TestNewTitler_NilGuards (B-03): all three constructor inputs must panic
// when nil/empty. Table-driven with recover() so a missing panic fails the
// case loudly.
func TestNewTitler_NilGuards(t *testing.T) {
	cases := []struct {
		name   string
		router chatCaller
		repo   domain.ConversationRepository
		model  string
	}{
		{"nil router", nil, &fakeConvRepo{}, "test-model"},
		{"nil repo", &fakeRouter{}, nil, "test-model"},
		{"empty model", &fakeRouter{}, &fakeConvRepo{}, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			defer func() {
				r := recover()
				if r == nil {
					t.Fatalf("expected panic for %q, got nil", c.name)
				}
				msg, ok := r.(string)
				if !ok || msg == "" {
					t.Fatalf("expected non-empty panic message for %q, got %v", c.name, r)
				}
				if !strings.Contains(msg, "NewTitler:") {
					t.Fatalf("expected panic message to start with NewTitler:, got %q", msg)
				}
			}()
			_ = NewTitler(c.router, c.repo, c.model)
		})
	}
}

func TestGenerateAndSave_Success(t *testing.T) {
	captureLogs(t)

	router := &fakeRouter{returnContent: "Запланировать пост"}
	repo := &fakeConvRepo{}
	tt := NewTitler(router, repo, "test-model")

	tt.GenerateAndSave(context.Background(), "biz-1", "conv-1", "помоги", "конечно")

	if len(repo.updateCalls) != 1 {
		t.Fatalf("UpdateTitleIfPending calls: got %d, want 1", len(repo.updateCalls))
	}
	if repo.updateCalls[0].ID != "conv-1" {
		t.Fatalf("Update id got=%q want=%q", repo.updateCalls[0].ID, "conv-1")
	}
	if repo.updateCalls[0].Title != "Запланировать пост" {
		t.Fatalf("title got=%q want=%q", repo.updateCalls[0].Title, "Запланировать пост")
	}
}

func TestGenerateAndSave_LLMError(t *testing.T) {
	captureLogs(t)

	router := &fakeRouter{returnErr: errors.New("provider down")}
	repo := &fakeConvRepo{}
	tt := NewTitler(router, repo, "test-model")

	tt.GenerateAndSave(context.Background(), "biz-1", "conv-1", "u", "a")

	if len(repo.updateCalls) != 0 {
		t.Fatalf("UpdateTitleIfPending must NOT be called on llm error; got %d calls", len(repo.updateCalls))
	}
}

func TestGenerateAndSave_EmptyResponse(t *testing.T) {
	captureLogs(t)

	router := &fakeRouter{returnContent: ""}
	repo := &fakeConvRepo{}
	tt := NewTitler(router, repo, "test-model")

	tt.GenerateAndSave(context.Background(), "biz-1", "conv-1", "u", "a")

	if len(repo.updateCalls) != 0 {
		t.Fatalf("UpdateTitleIfPending must NOT be called on empty response; got %d calls", len(repo.updateCalls))
	}
}

func TestGenerateAndSave_PIIReject_Terminal(t *testing.T) {
	buf := captureLogs(t)

	// Generated title contains an email — post-hoc PII gate must reject it
	// and the terminal "Untitled chat <day> <month>" fallback must be
	// written under the SAME atomic guard (D-05 + D-13).
	router := &fakeRouter{returnContent: "Связь user@example.com и +7 495 1234567"}
	repo := &fakeConvRepo{}
	tt := NewTitler(router, repo, "test-model")

	tt.GenerateAndSave(context.Background(), "biz-1", "conv-1", "u", "a")

	if len(repo.updateCalls) != 1 {
		t.Fatalf("expected exactly one Update call (terminal write); got %d", len(repo.updateCalls))
	}
	if !strings.HasPrefix(repo.updateCalls[0].Title, "Untitled chat ") {
		t.Fatalf("terminal title got=%q want prefix %q", repo.updateCalls[0].Title, "Untitled chat ")
	}
	// Log line should reference the regex_class but never the matched substring.
	captured := buf.String()
	if !strings.Contains(captured, "regex_class") {
		t.Fatalf("expected regex_class field in log output, got: %s", captured)
	}
	if strings.Contains(captured, "user@example.com") {
		t.Fatalf("PII leak in logs: matched substring appeared in log output: %s", captured)
	}
}

func TestGenerateAndSave_ManualWonRace(t *testing.T) {
	buf := captureLogs(t)

	router := &fakeRouter{returnContent: "Заголовок"}
	repo := &fakeConvRepo{updateRetErr: domain.ErrConversationNotFound}
	tt := NewTitler(router, repo, "test-model")

	tt.GenerateAndSave(context.Background(), "biz-1", "conv-1", "u", "a")

	if len(repo.updateCalls) != 1 {
		t.Fatalf("expected one UpdateTitleIfPending call (no-op surfaced via err); got %d", len(repo.updateCalls))
	}
	captured := buf.String()
	if !strings.Contains(captured, "manual_won_race") {
		t.Fatalf("expected manual_won_race outcome in log output, got: %q", captured)
	}
	// Manual-won-race is INFO level, NOT WARN.
	if !strings.Contains(captured, "level=INFO") {
		t.Fatalf("expected INFO level for manual_won_race, got: %q", captured)
	}
}

func TestGenerateAndSave_PersistError(t *testing.T) {
	captureLogs(t)

	router := &fakeRouter{returnContent: "Заголовок"}
	repo := &fakeConvRepo{updateRetErr: errors.New("mongo unreachable")}
	tt := NewTitler(router, repo, "test-model")

	tt.GenerateAndSave(context.Background(), "biz-1", "conv-1", "u", "a")

	if len(repo.updateCalls) != 1 {
		t.Fatalf("expected one UpdateTitleIfPending call; got %d", len(repo.updateCalls))
	}
}

// TestGenerateAndSave_LogShape (Landmine 6 / Pitfall 8 / TITLE-07):
// negative regression test — capture log output and assert that NONE of the
// banned PII substrings AND the original chat content AND the generated
// title appear in any log line.
func TestGenerateAndSave_LogShape(t *testing.T) {
	buf := captureLogs(t)

	const piiUserMsg = "моя почта user@x.ru а карта 4111111111111111"
	const piiAssistant = "перезвоню по +7 (495) 123-45-67"
	const generatedTitle = "Контакты клиента"

	router := &fakeRouter{returnContent: generatedTitle}
	repo := &fakeConvRepo{}
	tt := NewTitler(router, repo, "test-model")

	tt.GenerateAndSave(context.Background(), "biz-1", "conv-1", piiUserMsg, piiAssistant)

	captured := buf.String()
	// Negative assertions: NONE of these substrings may appear in any log line.
	bannedSubstrings := []string{
		"user@x.ru",
		"4111111111111111",
		"+7 (495) 123-45-67",
		piiUserMsg,
		piiAssistant,
		generatedTitle, // generated title must also not appear in logs (TITLE-07)
	}
	for _, s := range bannedSubstrings {
		if strings.Contains(captured, s) {
			t.Fatalf("Log shape violation (TITLE-07 / Pitfall 8): captured logs contain banned substring %q. Logs: %s", s, captured)
		}
	}
	// Positive assertion: structured metadata fields are present.
	for _, s := range []string{"conversation_id", "business_id", "prompt_length", "response_length"} {
		if !strings.Contains(captured, s) {
			t.Fatalf("Expected structured metadata field %q in log output, got: %s", s, captured)
		}
	}
}

// TestGenerateAndSave_PreRedact (D-14): the user message reaching the cheap
// LLM must be redacted; the raw email substring must not appear, and the
// "[Скрыто]" placeholder must be present.
func TestGenerateAndSave_PreRedact(t *testing.T) {
	captureLogs(t)

	router := &fakeRouter{returnContent: "ok"}
	repo := &fakeConvRepo{}
	tt := NewTitler(router, repo, "test-model")

	tt.GenerateAndSave(context.Background(), "biz-1", "conv-1", "пиши на user@x.ru", "ok")

	if router.lastReq == nil {
		t.Fatalf("router.Chat was not invoked")
	}
	var userPromptContent string
	for _, m := range router.lastReq.Messages {
		if m.Role == "user" {
			userPromptContent = m.Content
			break
		}
	}
	if userPromptContent == "" {
		t.Fatalf("no user-role message found in lastReq.Messages: %+v", router.lastReq.Messages)
	}
	if strings.Contains(userPromptContent, "user@x.ru") {
		t.Fatalf("Pre-redact failed: user message in LLM prompt still contains raw email: %q", userPromptContent)
	}
	if !strings.Contains(userPromptContent, "[Скрыто]") {
		t.Fatalf("Pre-redact failed: expected [Скрыто] placeholder in prompt, got: %q", userPromptContent)
	}
}
