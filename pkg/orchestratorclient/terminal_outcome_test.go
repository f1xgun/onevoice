package orchestratorclient

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/sse"
)

func TestStreamSSE_Termination(t *testing.T) {
	for _, tc := range []struct {
		name, ending string
		wantErr      bool
	}{
		{name: "done", ending: "data: {\"type\":\"done\"}\n\n"},
		{name: "explicit error", ending: "data: {\"type\":\"error\",\"code\":\"PROVIDER_ERROR\"}\n\n"},
		{name: "approval pause", ending: "data: {\"type\":\"tool_approval_required\"}\n\n"},
		{name: "scanner error", ending: strings.Repeat("x", sseScannerBufferBytes), wantErr: true},
		{name: "EOF without done", wantErr: true},
	} {
		for _, callback := range []bool{false, true} {
			t.Run(tc.name+map[bool]string{false: "/no callback", true: "/callback"}[callback], func(t *testing.T) {
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					_, _ = io.WriteString(w, "data: {\"type\":\"text\",\"content\":\"Partial\"}\n\n"+tc.ending)
				}))
				defer server.Close()
				recorder := httptest.NewRecorder()
				req := StreamSSERequest{ConversationID: "conversation", Writer: recorder}
				var events []sse.Event
				if callback {
					req.OnEvent = func(ev sse.Event) { events = append(events, ev) }
				}
				err := New(server.URL, server.Client()).StreamSSE(context.Background(), req)
				if tc.wantErr {
					require.Error(t, err)
					assert.Contains(t, recorder.Body.String(), "STREAM_INTERRUPTED")
					if tc.ending == "" {
						assert.ErrorIs(t, err, io.ErrUnexpectedEOF)
					}
					if callback {
						require.NotEmpty(t, events)
						assert.Equal(t, "STREAM_INTERRUPTED", events[len(events)-1].Code)
					}
				} else {
					require.NoError(t, err)
					assert.NotContains(t, recorder.Body.String(), "STREAM_INTERRUPTED")
				}
			})
		}
	}
}
