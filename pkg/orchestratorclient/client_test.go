package orchestratorclient

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/f1xgun/onevoice/pkg/logger"
	"github.com/f1xgun/onevoice/pkg/sse"
	"github.com/f1xgun/onevoice/pkg/tools"
)

func TestStreamChat_PostsToConversationURL(t *testing.T) {
	var (
		gotMethod string
		gotPath   string
		gotBody   []byte
		gotHeader string
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		gotHeader = r.Header.Get("X-Correlation-Id")
		gotBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"type\":\"done\"}\n\n"))
	}))
	defer srv.Close()

	client := New(srv.URL, srv.Client())
	resp, err := client.StreamChat(context.Background(), "conv-1", []byte(`{"message":"hi"}`), map[string]string{
		"X-Correlation-Id": "abc-123",
	})
	if err != nil {
		t.Fatalf("StreamChat: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if gotMethod != http.MethodPost {
		t.Errorf("method = %q, want POST", gotMethod)
	}
	if gotPath != "/chat/conv-1" {
		t.Errorf("path = %q, want /chat/conv-1", gotPath)
	}
	if string(gotBody) != `{"message":"hi"}` {
		t.Errorf("body = %q, want JSON body forwarded verbatim", gotBody)
	}
	if gotHeader != "abc-123" {
		t.Errorf("X-Correlation-Id = %q, want abc-123", gotHeader)
	}
}

func TestStreamResume_PassesBatchIDAsQuery(t *testing.T) {
	var (
		gotPath  string
		gotQuery string
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"type\":\"done\"}\n\n"))
	}))
	defer srv.Close()

	client := New(srv.URL, srv.Client())
	resp, err := client.StreamResume(context.Background(), "conv-2", "batch-9", []byte(`{}`), nil)
	if err != nil {
		t.Fatalf("StreamResume: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if gotPath != "/chat/conv-2/resume" {
		t.Errorf("path = %q, want /chat/conv-2/resume", gotPath)
	}
	if !strings.Contains(gotQuery, "batch_id=batch-9") {
		t.Errorf("query = %q, want batch_id=batch-9", gotQuery)
	}
}

func TestListTools_ParsesEntries(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/internal/tools" {
			t.Errorf("path = %q, want /internal/tools", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[
			{"name":"telegram__send_channel_post","platform":"telegram","floor":"manual","editableFields":["text"],"description":"Send a Telegram channel post"},
			{"name":"vk__publish_post","platform":"vk","floor":"manual","editableFields":["text"],"description":"Publish a VK post"}
		]`))
	}))
	defer srv.Close()

	client := New(srv.URL, srv.Client())
	got, err := client.ListTools(context.Background(), "")
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len(got) = %d, want 2", len(got))
	}
	if got[0].Name != tools.TelegramSendChannelPost || got[0].Platform != "telegram" || got[0].Floor != "manual" {
		t.Errorf("got[0] = %+v, want telegram entry", got[0])
	}
	if got[1].Name != tools.VKPublishPost {
		t.Errorf("got[1].Name = %q, want vk__publish_post", got[1].Name)
	}
}

// TestListTools_ForwardsAcceptLanguage verifies the per-locale tools
// projection round-trips correctly when the caller supplies a tag. The
// orchestrator's middleware.Locale resolves Accept-Language to the per-locale
// description; this test pins the wire contract from the API's cache side.
func TestListTools_ForwardsAcceptLanguage(t *testing.T) {
	var gotAcceptLang string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAcceptLang = r.Header.Get("Accept-Language")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[]`))
	}))
	defer srv.Close()

	client := New(srv.URL, srv.Client())
	if _, err := client.ListTools(context.Background(), "en"); err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	if gotAcceptLang != "en" {
		t.Errorf("Accept-Language = %q, want en", gotAcceptLang)
	}

	// Empty acceptLanguage must NOT set the header at all (so the
	// orchestrator's middleware falls back to its own default).
	gotAcceptLang = "<unset>"
	if _, err := client.ListTools(context.Background(), ""); err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	if gotAcceptLang != "" {
		t.Errorf("Accept-Language = %q, want empty (header not set)", gotAcceptLang)
	}
}

func TestListToolNames_ReturnsSet(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/internal/tools/names" {
			t.Errorf("path = %q, want /internal/tools/names", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"names": []string{tools.TelegramSendChannelPost, tools.VKPublishPost},
		})
	}))
	defer srv.Close()

	client := New(srv.URL, srv.Client())
	got, err := client.ListToolNames(context.Background())
	if err != nil {
		t.Fatalf("ListToolNames: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len(got) = %d, want 2", len(got))
	}
	if _, ok := got[tools.TelegramSendChannelPost]; !ok {
		t.Error("missing telegram__send_channel_post in set")
	}
	if _, ok := got[tools.VKPublishPost]; !ok {
		t.Error("missing vk__publish_post in set")
	}
}

func TestNew_NilHTTPClient_DefaultsToHTTPDefaultClient(t *testing.T) {
	c := New("http://example.com/", nil)
	if c.httpClient != http.DefaultClient {
		t.Errorf("nil httpClient should fall back to http.DefaultClient; got %p (default %p)", c.httpClient, http.DefaultClient)
	}
	// Trailing slash trimmed.
	if c.baseURL != "http://example.com" {
		t.Errorf("baseURL = %q, want http://example.com (trailing slash trimmed)", c.baseURL)
	}
}

func TestDraftReply_PostsAndDecodes(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/internal/draft-reply" {
			t.Errorf("path = %q, want /internal/draft-reply", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Errorf("method = %q, want POST", r.Method)
		}
		var got DraftReplyRequest
		_ = json.NewDecoder(r.Body).Decode(&got)
		if got.ReviewText != "hello" {
			t.Errorf("reviewText = %q, want hello", got.ReviewText)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(DraftReplyResponse{DraftReply: "thanks", Provider: "openrouter"})
	}))
	defer srv.Close()

	client := New(srv.URL, srv.Client())
	got, err := client.DraftReply(context.Background(), DraftReplyRequest{
		BusinessID: "b1", BusinessName: "Cafe", Platform: "vk", ReviewText: "hello", Rating: 5,
	})
	if err != nil {
		t.Fatalf("DraftReply: %v", err)
	}
	if got.DraftReply != "thanks" {
		t.Errorf("DraftReply = %q, want thanks", got.DraftReply)
	}
	if got.Provider != "openrouter" {
		t.Errorf("Provider = %q, want openrouter", got.Provider)
	}
}

// --- StreamSSE ---------------------------------------------------------------

// recordingFlusher is an http.ResponseWriter+Flusher recorder used to inspect
// what StreamSSE actually wrote downstream. httptest.ResponseRecorder doesn't
// implement Flusher, so StreamSSE would error out — this stub does.
type recordingFlusher struct {
	headers http.Header
	status  int
	body    strings.Builder
}

func newRecordingFlusher() *recordingFlusher {
	return &recordingFlusher{headers: make(http.Header)}
}

func (r *recordingFlusher) Header() http.Header         { return r.headers }
func (r *recordingFlusher) WriteHeader(s int)           { r.status = s }
func (r *recordingFlusher) Write(b []byte) (int, error) { return r.body.Write(b) }
func (r *recordingFlusher) Flush()                      {}

// nonFlusher is an http.ResponseWriter that does NOT implement http.Flusher —
// used to assert StreamSSE's fail-fast guard.
type nonFlusher struct{}

func (nonFlusher) Header() http.Header         { return http.Header{} }
func (nonFlusher) WriteHeader(int)             {}
func (nonFlusher) Write(b []byte) (int, error) { return len(b), nil }

func TestStreamSSE_FreshChat_ForwardsBytesAndSetsSSEHeaders(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/conv-1" {
			t.Errorf("path = %q, want /chat/conv-1", r.URL.Path)
		}
		_, _ = w.Write([]byte("data: {\"type\":\"text\",\"content\":\"hi\"}\ndata: {\"type\":\"done\"}\n"))
	}))
	defer srv.Close()

	rec := newRecordingFlusher()
	err := New(srv.URL, srv.Client()).StreamSSE(context.Background(), StreamSSERequest{
		ConversationID: "conv-1",
		Body:           []byte(`{"message":"hi"}`),
		Writer:         rec,
	})
	if err != nil {
		t.Fatalf("StreamSSE: %v", err)
	}
	if rec.headers.Get("Content-Type") != "text/event-stream" {
		t.Errorf("Content-Type = %q", rec.headers.Get("Content-Type"))
	}
	if rec.headers.Get("X-Accel-Buffering") != "no" {
		t.Errorf("X-Accel-Buffering = %q, want no", rec.headers.Get("X-Accel-Buffering"))
	}
	if rec.status != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.status)
	}
	if !strings.Contains(rec.body.String(), "data: {\"type\":\"text\",\"content\":\"hi\"}") {
		t.Errorf("body missing forwarded frame; got %q", rec.body.String())
	}
}

func TestStreamSSE_ResumePath_SetsBatchIDQuery(t *testing.T) {
	var gotPath, gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotQuery = r.URL.RawQuery
		_, _ = w.Write([]byte("data: {\"type\":\"done\"}\n"))
	}))
	defer srv.Close()

	err := New(srv.URL, srv.Client()).StreamSSE(context.Background(), StreamSSERequest{
		ConversationID: "conv-1",
		BatchID:        "batch-9",
		Writer:         newRecordingFlusher(),
	})
	if err != nil {
		t.Fatalf("StreamSSE: %v", err)
	}
	if gotPath != "/chat/conv-1/resume" {
		t.Errorf("path = %q, want /chat/conv-1/resume", gotPath)
	}
	if gotQuery != "batch_id=batch-9" {
		t.Errorf("query = %q, want batch_id=batch-9", gotQuery)
	}
}

func TestStreamSSE_OnEvent_InvokedPerParsedFrame(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(
			"data: {\"type\":\"text\",\"content\":\"hello\"}\n" +
				"data: {\"type\":\"tool_call\",\"tool_name\":\"t__a\"}\n" +
				"data: {\"type\":\"done\"}\n",
		))
	}))
	defer srv.Close()

	var got []string
	err := New(srv.URL, srv.Client()).StreamSSE(context.Background(), StreamSSERequest{
		ConversationID: "conv-1",
		Writer:         newRecordingFlusher(),
		OnEvent:        func(ev sse.Event) { got = append(got, ev.Type) },
	})
	if err != nil {
		t.Fatalf("StreamSSE: %v", err)
	}
	wantTypes := []string{"text", "tool_call", "done"}
	if len(got) != len(wantTypes) {
		t.Fatalf("OnEvent calls = %v, want %v", got, wantTypes)
	}
	for i, want := range wantTypes {
		if got[i] != want {
			t.Errorf("OnEvent[%d] = %q, want %q", i, got[i], want)
		}
	}
}

func TestStreamSSE_MalformedFrame_SkipsOnEventButForwardsRaw(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(
			"data: {garbage}\n" +
				"data: {\"type\":\"done\"}\n",
		))
	}))
	defer srv.Close()

	var got []string
	rec := newRecordingFlusher()
	err := New(srv.URL, srv.Client()).StreamSSE(context.Background(), StreamSSERequest{
		ConversationID: "conv-1",
		Writer:         rec,
		OnEvent:        func(ev sse.Event) { got = append(got, ev.Type) },
	})
	if err != nil {
		t.Fatalf("StreamSSE: %v", err)
	}
	// Malformed line must still appear on the wire (FE may want to log it).
	if !strings.Contains(rec.body.String(), "{garbage}") {
		t.Errorf("body missing malformed frame; got %q", rec.body.String())
	}
	// Only the well-formed "done" frame triggers OnEvent.
	if len(got) != 1 || got[0] != "done" {
		t.Errorf("OnEvent calls = %v, want [done]", got)
	}
}

func TestStreamSSE_CorrelationIDFromCtx_PropagatedAsHeader(t *testing.T) {
	var gotCorrID string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotCorrID = r.Header.Get("X-Correlation-Id")
		_, _ = w.Write([]byte("data: {\"type\":\"done\"}\n"))
	}))
	defer srv.Close()

	ctx := logger.WithCorrelationID(context.Background(), "corr-abc")
	err := New(srv.URL, srv.Client()).StreamSSE(ctx, StreamSSERequest{
		ConversationID: "conv-1",
		Writer:         newRecordingFlusher(),
	})
	if err != nil {
		t.Fatalf("StreamSSE: %v", err)
	}
	if gotCorrID != "corr-abc" {
		t.Errorf("X-Correlation-Id = %q, want corr-abc", gotCorrID)
	}
}

func TestStreamSSE_CallerHeaderWinsOverCtxCorrelation(t *testing.T) {
	// If the caller supplies an X-Correlation-ID in Headers, StreamSSE must
	// NOT overwrite it with the ctx-derived id — the chatturn paths
	// pre-merge their own headers and that map is authoritative.
	var gotCorrID string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotCorrID = r.Header.Get("X-Correlation-Id")
		_, _ = w.Write([]byte("data: {\"type\":\"done\"}\n"))
	}))
	defer srv.Close()

	ctx := logger.WithCorrelationID(context.Background(), "from-ctx")
	err := New(srv.URL, srv.Client()).StreamSSE(ctx, StreamSSERequest{
		ConversationID: "conv-1",
		Writer:         newRecordingFlusher(),
		Headers:        map[string]string{"X-Correlation-ID": "from-caller"},
	})
	if err != nil {
		t.Fatalf("StreamSSE: %v", err)
	}
	if gotCorrID != "from-caller" {
		t.Errorf("X-Correlation-Id = %q, want from-caller (caller override wins)", gotCorrID)
	}
}

func TestStreamSSE_WriterMissingFlusher_FailsFastNoWrites(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("upstream must NOT be contacted when Writer lacks Flusher")
	}))
	defer srv.Close()

	err := New(srv.URL, srv.Client()).StreamSSE(context.Background(), StreamSSERequest{
		ConversationID: "conv-1",
		Writer:         nonFlusher{},
	})
	if err == nil {
		t.Fatal("StreamSSE: want error for non-flusher writer, got nil")
	}
	if !strings.Contains(err.Error(), "Flusher") {
		t.Errorf("error = %v, want mention of Flusher", err)
	}
}

func TestStreamSSE_ClientGone_StopsWritesButDrainsUpstream(t *testing.T) {
	// Under OrchCtxBudget > 0, a cancellation on the caller ctx must STOP
	// forwarding to Writer but MUST keep draining the upstream so the
	// orchestrator's side effects can run to completion. This is the
	// chatturn lifecycle invariant — a client navigating away mid-stream
	// must not abort the LLM call.
	const totalFrames = 8
	var emittedFrames int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f, _ := w.(http.Flusher)
		for i := 0; i < totalFrames; i++ {
			_, _ = w.Write([]byte("data: {\"type\":\"text\",\"content\":\"x\"}\n"))
			f.Flush()
			atomic.AddInt32(&emittedFrames, 1)
			time.Sleep(2 * time.Millisecond)
		}
	}))
	defer srv.Close()

	rec := newRecordingFlusher()
	clientCtx, cancel := context.WithCancel(context.Background())

	var seenViaOnEvent int32
	go func() {
		// Cancel partway through so some frames write to the recorder and
		// the rest are drained-but-not-written.
		time.Sleep(5 * time.Millisecond)
		cancel()
	}()

	err := New(srv.URL, srv.Client()).StreamSSE(clientCtx, StreamSSERequest{
		ConversationID: "conv-1",
		Writer:         rec,
		OrchCtxBudget:  5 * time.Second,
		OnEvent:        func(ev sse.Event) { atomic.AddInt32(&seenViaOnEvent, 1) },
	})
	if err != nil {
		t.Fatalf("StreamSSE: %v", err)
	}

	// OnEvent fires for EVERY drained frame regardless of clientGone — proves
	// the drain ran to completion even after cancel().
	if got := atomic.LoadInt32(&seenViaOnEvent); int(got) != totalFrames {
		t.Errorf("OnEvent count = %d, want %d (full drain expected)", got, totalFrames)
	}
	// At least one frame should NOT have been forwarded to Writer — proving
	// clientGone actually suppressed writes. (Tightening the assertion
	// further would race with the goroutine scheduler.)
	writtenLines := strings.Count(rec.body.String(), "data: ")
	if writtenLines >= totalFrames {
		t.Errorf("writer received %d frames, expected at least one suppressed under clientGone", writtenLines)
	}
}
