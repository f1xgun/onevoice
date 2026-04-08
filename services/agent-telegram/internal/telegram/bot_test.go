package telegram

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newMockTelegramServer creates a mock Telegram Bot API server.
// handler is called for all non-getMe requests.
func newMockTelegramServer(t *testing.T, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Handle getMe (called during BotAPI initialization)
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
	err := bot.SendMessage(-1001234567890, "Hello!")

	require.NoError(t, err)
	assert.Contains(t, capturedPath, "/sendMessage")
	assert.Equal(t, "-1001234567890", capturedChatID)
	assert.Equal(t, "Hello!", capturedText)
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
	err := bot.SendMessage(999999, "Hello!")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "chat not found")
}

func TestSendPhoto_Success(t *testing.T) {
	// Photo server that serves an image
	photoSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write([]byte("fake-jpeg-data"))
	}))
	defer photoSrv.Close()

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

	// Override the package-level photoHTTPClient for testing
	origClient := photoHTTPClient
	photoHTTPClient = photoSrv.Client()
	t.Cleanup(func() { photoHTTPClient = origClient })

	err := bot.SendPhoto(-1001234567890, photoSrv.URL+"/image.jpg", "Nice pic!")

	require.NoError(t, err)
	assert.Contains(t, capturedPath, "/sendPhoto")
	assert.Contains(t, capturedContentType, "multipart/form-data", "photo should be sent as multipart upload")
}

func TestSendPhoto_DownloadFails(t *testing.T) {
	// Photo server that returns 404
	photoSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer photoSrv.Close()

	srv := newMockTelegramServer(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("should not call Telegram API when photo download fails")
	})
	defer srv.Close()

	bot := newTestBot(t, srv)

	origClient := photoHTTPClient
	photoHTTPClient = photoSrv.Client()
	t.Cleanup(func() { photoHTTPClient = origClient })

	err := bot.SendPhoto(-1001234567890, photoSrv.URL+"/missing.jpg", "caption")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "download photo")
}

func TestSendPhoto_InvalidURL(t *testing.T) {
	srv := newMockTelegramServer(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("should not call Telegram API when photo URL is invalid")
	})
	defer srv.Close()

	bot := newTestBot(t, srv)
	err := bot.SendPhoto(-1001234567890, "://invalid-url", "caption")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "download photo")
}

func TestSendMessage_EmptyText(t *testing.T) {
	srv := newMockTelegramServer(t, func(w http.ResponseWriter, r *http.Request) {
		// Telegram API rejects empty messages
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"ok":          false,
			"description": "Bad Request: message text is empty",
			"error_code":  400,
		})
	})
	defer srv.Close()

	bot := newTestBot(t, srv)
	err := bot.SendMessage(-1001234567890, "")

	require.Error(t, err)
}
