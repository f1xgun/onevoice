package telegram

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPostMutationsHTTP(t *testing.T) {
	for _, tc := range []struct {
		name, chat, field, text string
		caption, remove         bool
	}{
		{name: "editMessageText", chat: "@own", field: "text", text: "Updated"},
		{name: "editMessageCaption", chat: "-100123", field: "caption", caption: true},
		{name: "deleteMessage", chat: "@own", remove: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := newMockTelegramServer(t, func(w http.ResponseWriter, r *http.Request) {
				require.True(t, strings.HasSuffix(r.URL.Path, "/"+tc.name))
				require.NoError(t, r.ParseForm())
				require.Equal(t, tc.chat, r.FormValue("chat_id"))
				require.Equal(t, "42", r.FormValue("message_id"))
				if tc.field != "" {
					require.Contains(t, r.Form, tc.field)
					require.Equal(t, tc.text, r.FormValue(tc.field))
				}
				if tc.remove {
					_, _ = fmt.Fprint(w, `{"ok":true,"result":true}`)
				} else {
					_, _ = fmt.Fprint(w, `{"ok":true,"result":{"message_id":42,"chat":{"id":-100123,"username":"own"}}}`)
				}
			})
			defer srv.Close()
			bot := newTestBot(t, srv)
			if tc.remove {
				require.NoError(t, bot.DeleteMessage(tc.chat, 42))
			} else {
				receipt, err := bot.EditMessage(tc.chat, 42, tc.text, tc.caption)
				require.NoError(t, err)
				require.Equal(t, SentMessage{MessageID: 42, ChatUsername: "own"}, receipt)
			}
		})
	}
}

func TestPostMutationTelegramError(t *testing.T) {
	srv := newMockTelegramServer(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w, `{"ok":false,"error_code":400,"description":"Bad Request: message can't be deleted"}`)
	})
	defer srv.Close()
	require.ErrorContains(t, newTestBot(t, srv).DeleteMessage("@own", 42), "can't be deleted")
}

func TestEditMessageAlreadyUpdated(t *testing.T) {
	srv := newMockTelegramServer(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w, `{"ok":false,"error_code":400,"description":"Bad Request: message is not modified"}`)
	})
	defer srv.Close()
	receipt, err := newTestBot(t, srv).EditMessage("@own", 42, "unchanged", false)
	require.NoError(t, err)
	require.Equal(t, 42, receipt.MessageID)
}
