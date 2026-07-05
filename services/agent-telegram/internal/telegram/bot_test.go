package telegram

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/safefetch"
)

// newMockTelegramServer creates a mock Telegram Bot API server.
// handler is called for all non-getMe requests.
func newMockTelegramServer(t *testing.T, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/getMe") {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"ok": true,
				"result": map[string]interface{}{
					"id":         12345,
					"is_bot":     true,
					"first_name": "TestBot",
					"username":   "test_bot",
				},
			})
			return
		}
		handler(w, r)
	}))
}

// newTestBot creates a Bot connected to a mock Telegram API server.
func newTestBot(t *testing.T, srv *httptest.Server) *Bot {
	t.Helper()
	api, err := tgbotapi.NewBotAPIWithClient(
		"test-token",
		srv.URL+"/bot%s/%s",
		srv.Client(),
	)
	require.NoError(t, err)
	return &Bot{api: api}
}

func TestSendMessage_Success(t *testing.T) {
	var capturedPath string
	var capturedChatID string
	var capturedText string

	srv := newMockTelegramServer(t, func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		_ = r.ParseForm()
		capturedChatID = r.FormValue("chat_id")
		capturedText = r.FormValue("text")

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"ok": true,
			"result": map[string]interface{}{
				"message_id": 42,
				"chat":       map[string]interface{}{"id": -1001234567890},
				"text":       "Hello!",
			},
		})
	})
	defer srv.Close()

	bot := newTestBot(t, srv)
	err := bot.SendMessage("-1001234567890", "Hello!")

	require.NoError(t, err)
	assert.Contains(t, capturedPath, "/sendMessage")
	assert.Equal(t, "-1001234567890", capturedChatID)
	assert.Equal(t, "Hello!", capturedText)
}

func TestSendMessage_PublicChannelUsername(t *testing.T) {
	var capturedChatID string
	var capturedText string

	srv := newMockTelegramServer(t, func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		capturedChatID = r.FormValue("chat_id")
		capturedText = r.FormValue("text")

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"ok":     true,
			"result": map[string]interface{}{"message_id": 7},
		})
	})
	defer srv.Close()

	bot := newTestBot(t, srv)
	err := bot.SendMessage("@onevoice_test", "Hello, public channel!")

	require.NoError(t, err)
	assert.Equal(t, "@onevoice_test", capturedChatID, "public @username must be sent verbatim as chat_id")
	assert.Equal(t, "Hello, public channel!", capturedText)
}

func TestSendMessage_APIError(t *testing.T) {
	srv := newMockTelegramServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"ok":          false,
			"description": "Bad Request: chat not found",
			"error_code":  400,
		})
	})
	defer srv.Close()

	bot := newTestBot(t, srv)
	err := bot.SendMessage("999999", "Hello!")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "chat not found")
}

// stubFetcher swaps photoFetcher for the test and restores it on cleanup.
func stubFetcher(t *testing.T, fn fetcherFunc) {
	t.Helper()
	orig := photoFetcher
	photoFetcher = fn
	t.Cleanup(func() { photoFetcher = orig })
}

// fetcherFunc adapts a function to the imageFetcher interface.
type fetcherFunc func(ctx context.Context, rawURL string) ([]byte, string, error)

func (f fetcherFunc) Get(ctx context.Context, rawURL string) (body []byte, contentType string, err error) {
	return f(ctx, rawURL)
}

func TestSendPhoto_Success(t *testing.T) {
	stubFetcher(t, func(_ context.Context, _ string) ([]byte, string, error) {
		return []byte("fake-jpeg-data"), "image/jpeg", nil
	})

	var capturedPath string
	var capturedContentType string

	srv := newMockTelegramServer(t, func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		capturedContentType = r.Header.Get("Content-Type")

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"ok": true,
			"result": map[string]interface{}{
				"message_id": 43,
				"chat":       map[string]interface{}{"id": -1001234567890},
			},
		})
	})
	defer srv.Close()

	bot := newTestBot(t, srv)
	err := bot.SendPhoto("-1001234567890", "https://images.example.test/image.jpg", "Nice pic!")

	require.NoError(t, err)
	assert.Contains(t, capturedPath, "/sendPhoto")
	assert.Contains(t, capturedContentType, "multipart/form-data", "photo should be sent as multipart upload")
}

func TestSendPhoto_DownloadFails(t *testing.T) {
	stubFetcher(t, func(_ context.Context, _ string) ([]byte, string, error) {
		return nil, "", fmt.Errorf("status 404")
	})

	srv := newMockTelegramServer(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("should not call Telegram API when photo download fails")
	})
	defer srv.Close()

	bot := newTestBot(t, srv)
	err := bot.SendPhoto("-1001234567890", "https://images.example.test/missing.jpg", "caption")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "download photo")
}

func TestSendPhoto_InvalidURL(t *testing.T) {
	srv := newMockTelegramServer(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("should not call Telegram API when photo URL is invalid")
	})
	defer srv.Close()

	bot := newTestBot(t, srv)
	err := bot.SendPhoto("-1001234567890", "://invalid-url", "caption")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "download photo")
}

func TestSendPhoto_RejectsNonImageContentType(t *testing.T) {
	stubFetcher(t, func(_ context.Context, _ string) ([]byte, string, error) {
		return []byte("<html>not an image</html>"), "text/html", nil
	})

	srv := newMockTelegramServer(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("should not call Telegram API when fetched content-type is not an image")
	})
	defer srv.Close()

	bot := newTestBot(t, srv)
	err := bot.SendPhoto("-1001234567890", "https://images.example.test/page.html", "caption")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "content-type")
}

// TestSendPhoto_RejectsSSRF asserts the real safefetch guard blocks
// LLM-supplied internal/non-https photo URLs before any download or Telegram
// API call happens. The mock Telegram handler fails the test if reached.
func TestSendPhoto_RejectsSSRF(t *testing.T) {
	srv := newMockTelegramServer(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("should not call Telegram API when photo URL is blocked by SSRF guard")
	})
	defer srv.Close()

	bot := newTestBot(t, srv)
	disallowed := []string{
		"http://example.com/photo.jpg",
		"https://127.0.0.1/photo.jpg",
		"https://169.254.169.254/latest/meta-data",
		"https://10.0.0.1/photo.jpg",
		"https://[::1]/photo.jpg",
	}
	for _, raw := range disallowed {
		err := bot.SendPhoto("-1001234567890", raw, "caption")
		require.Error(t, err, "SendPhoto(%q) must be rejected", raw)
		require.ErrorIs(t, err, safefetch.ErrUnsafeURL, "SendPhoto(%q) must fail closed with ErrUnsafeURL", raw)
		assert.Contains(t, err.Error(), "download photo")
	}
}

func TestGetReviews_SkipsMessageWithNilChat(t *testing.T) {
	var callCount int

	srv := newMockTelegramServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		callCount++
		if callCount > 1 {
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"ok":     true,
				"result": []interface{}{},
			})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"ok": true,
			"result": []interface{}{
				map[string]interface{}{
					"update_id": 1,
					"message": map[string]interface{}{
						"message_id": 100,
						"date":       1700000000,
						"text":       "review without a chat",
						"from":       map[string]interface{}{"id": 7, "first_name": "Alice"},
					},
				},
				map[string]interface{}{
					"update_id": 2,
					"message": map[string]interface{}{
						"message_id": 101,
						"date":       1700000001,
						"text":       "valid review",
						"chat":       map[string]interface{}{"id": -1001234567890, "type": "supergroup"},
						"from":       map[string]interface{}{"id": 8, "first_name": "Bob"},
					},
				},
			},
		})
	})
	defer srv.Close()

	bot := newTestBot(t, srv)

	var reviews []map[string]interface{}
	var err error
	require.NotPanics(t, func() {
		reviews, err = bot.GetReviews(0)
	}, "a message with a nil Chat must not panic")

	require.NoError(t, err)
	require.Len(t, reviews, 1, "the chat-less message must be skipped, only the valid one kept")
	assert.Equal(t, "-1001234567890_101", reviews[0]["id"])
	assert.Equal(t, "valid review", reviews[0]["text"])
}

func TestSendMessage_EmptyText(t *testing.T) {
	srv := newMockTelegramServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"ok":          false,
			"description": "Bad Request: message text is empty",
			"error_code":  400,
		})
	})
	defer srv.Close()

	bot := newTestBot(t, srv)
	err := bot.SendMessage("-1001234567890", "")

	require.Error(t, err)
}

// TestSendMessage_StalledServerTimesOut asserts the Bot API client carries its
// own overall deadline. tgbotapi v5 builds requests with http.NewRequest (no
// context), so the only thing that can unblock a send against a server that
// accepts the connection but never writes response headers is the client's own
// timeout. The mock server below answers getMe (so the bot constructs) but then
// blocks every send past the configured timeout. With the timeout in place the
// send returns a timeout error; reverting it makes this hang until the test's
// own deadline trips it.
func TestSendMessage_StalledServerTimesOut(t *testing.T) {
	const clientTimeout = 200 * time.Millisecond

	release := make(chan struct{})
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }

	srv := newMockTelegramServer(t, func(http.ResponseWriter, *http.Request) {
		<-release
	})
	defer srv.Close()
	defer unblock()

	client := newAPIClient(clientTimeout)
	api, err := tgbotapi.NewBotAPIWithClient("test-token", srv.URL+"/bot%s/%s", client)
	require.NoError(t, err)
	bot := &Bot{api: api}

	done := make(chan error, 1)
	go func() { done <- bot.SendMessage("-1001234567890", "Hello!") }()

	select {
	case sendErr := <-done:
		require.Error(t, sendErr, "stalled server must surface a timeout, not a nil error")
		assert.Contains(t, strings.ToLower(sendErr.Error()), "timeout",
			"send against a stalled server must fail with a timeout error")
	case <-time.After(5 * time.Second):
		t.Fatal("SendMessage blocked far past the client timeout — no overall deadline is set on the Bot API client")
	}
}

// TestReviewPoll_AllowedUpdatesUnchanged pins the review poll's allowed-updates
// set: adding the callback plane must NOT leak callback_query into GetReviews
// (which would surface a button tap as a false-positive review). The
// callback-only set is disjoint from the review set.
func TestReviewPoll_AllowedUpdatesUnchanged(t *testing.T) {
	assert.Equal(t, []string{"message", "edited_message"}, allowedUpdateTypes,
		"review poll allowed-updates must not include callback_query")
	assert.Equal(t, []string{"callback_query"}, callbackUpdateTypes,
		"callback poll must request only callback_query")
	for _, u := range callbackUpdateTypes {
		assert.NotContains(t, allowedUpdateTypes, u, "callback update type leaked into the review poll set")
	}
}

// TestPollCallbacks_DeliversAndAdvancesOffset proves the callback poller: it
// requests only callback_query updates, delivers each callback_query with the
// authentic from.id + opaque data, advances the offset past processed updates
// (so the same callback is not re-delivered), and returns when ctx is canceled.
func TestPollCallbacks_DeliversAndAdvancesOffset(t *testing.T) {
	var mu sync.Mutex
	var offsets []int
	var allowed []string
	served := false

	srv := newMockTelegramServer(t, func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		mu.Lock()
		offsets = append(offsets, atoiSafe(r.FormValue("offset")))
		if a := r.FormValue("allowed_updates"); a != "" {
			allowed = append(allowed, a)
		}
		first := !served
		served = true
		mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		if first {
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"ok": true,
				"result": []map[string]interface{}{{
					"update_id": 100,
					"callback_query": map[string]interface{}{
						"id":   "cbq-1",
						"from": map[string]interface{}{"id": 987654321, "is_bot": false, "first_name": "Owner"},
						"data": "v1:batch-xyz:a:deadbeef",
						"message": map[string]interface{}{
							"message_id": 1,
							"chat":       map[string]interface{}{"id": 555},
						},
					},
				}},
			})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "result": []interface{}{}})
	})
	defer srv.Close()

	bot := newTestBot(t, srv)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	got := make(chan CallbackEvent, 1)
	done := make(chan error, 1)
	go func() {
		done <- bot.PollCallbacks(ctx, func(cq CallbackEvent) {
			select {
			case got <- cq:
			default:
			}
		})
	}()

	select {
	case cq := <-got:
		assert.Equal(t, "cbq-1", cq.QueryID)
		assert.Equal(t, int64(987654321), cq.FromID)
		assert.Equal(t, "v1:batch-xyz:a:deadbeef", cq.Data)
		assert.Equal(t, int64(555), cq.ChatID)
	case <-time.After(3 * time.Second):
		t.Fatal("PollCallbacks did not deliver the callback within the deadline")
	}

	cancel()
	select {
	case err := <-done:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(3 * time.Second):
		t.Fatal("PollCallbacks did not return after ctx cancel")
	}

	mu.Lock()
	defer mu.Unlock()
	require.NotEmpty(t, allowed)
	assert.Contains(t, allowed[0], "callback_query", "poller must request callback_query updates")
	require.GreaterOrEqual(t, len(offsets), 2)
	assert.Equal(t, 0, offsets[0], "first poll starts at offset 0")
	assert.Equal(t, 101, offsets[1], "offset must advance past the delivered update_id 100")
}

// TestAnswerCallback_HitsAnswerCallbackQuery proves AnswerCallback issues the
// answerCallbackQuery Bot API call with the callback_query id, so the tapper's
// button spinner is acknowledged.
func TestAnswerCallback_HitsAnswerCallbackQuery(t *testing.T) {
	var capturedPath, capturedID, capturedText string
	srv := newMockTelegramServer(t, func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		capturedPath = r.URL.Path
		capturedID = r.FormValue("callback_query_id")
		capturedText = r.FormValue("text")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "result": true})
	})
	defer srv.Close()

	bot := newTestBot(t, srv)
	err := bot.AnswerCallback("cbq-42", "Одобрено", false)

	require.NoError(t, err)
	assert.Contains(t, capturedPath, "/answerCallbackQuery")
	assert.Equal(t, "cbq-42", capturedID)
	assert.Equal(t, "Одобрено", capturedText)
}

// atoiSafe parses a base-10 int, returning 0 on any error (test helper for
// reading form values).
func atoiSafe(s string) int {
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0
		}
		n = n*10 + int(c-'0')
	}
	return n
}
