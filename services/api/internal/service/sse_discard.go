package service

import (
	"io"
	"net/http"
)

// discardSSEWriter is an http.ResponseWriter + http.Flusher that drops every
// byte written to it. The chat-turn resume core (chatturn.Turn.ResumeApproved →
// runResumeStream → Orch.StreamSSE) proxies the orchestrator SSE stream to a
// ResponseWriter that must also implement http.Flusher; in the browser path that
// is the real client connection. An off-app Telegram approval has no client
// stream — the owner tapped a button, not opened an SSE — so the resume is driven
// headlessly against this sink. The approved tools STILL execute and the assistant
// message is STILL persisted (those are side effects of the stream loop, not of
// the bytes reaching a client); only the streamed text/events are discarded.
type discardSSEWriter struct {
	header http.Header
}

// newDiscardSSEWriter returns a ready discardSSEWriter with an initialized
// header map so Header() never returns nil.
func newDiscardSSEWriter() *discardSSEWriter {
	return &discardSSEWriter{header: make(http.Header)}
}

// Header returns a throwaway header map. The resume path sets SSE headers on it;
// nothing reads them back.
func (w *discardSSEWriter) Header() http.Header { return w.header }

// Write discards the bytes and reports success so the stream loop never treats
// the sink as a broken client and aborts early.
func (w *discardSSEWriter) Write(p []byte) (int, error) {
	return io.Discard.Write(p)
}

// WriteHeader is a no-op — there is no client status line to send.
func (w *discardSSEWriter) WriteHeader(int) {}

// Flush is a no-op — there is no buffered client connection to flush.
func (w *discardSSEWriter) Flush() {}
