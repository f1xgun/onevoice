package service

import (
	"context"
	"sync"

	"github.com/f1xgun/onevoice/pkg/llm"
)

// FakeChatCaller is exported for handler/integration tests that need to
// construct a real *Titler with a recordable LLM-call seam. It implements
// the package-private chatCaller interface (Go's structural typing matches
// by method set; no explicit "implements" needed).
//
// B-02 alignment: this is the ONE canonical mocking seam for the LLM call
// in Phase 18. Handler tests do NOT introduce a parallel titlerCaller —
// they construct a real *Titler with FakeChatCaller and assert side
// effects on the conversation repo / inspect LastReq.
//
// This file ships with production code (no `_test.go` suffix) because Go
// forbids `_test.go` files from being imported by other packages. The
// FakeChatCaller type lives in the same package as Titler so it can
// satisfy the package-private chatCaller interface, but it's exported so
// handler tests in services/api/internal/handler can use it.
type FakeChatCaller struct {
	ReturnContent string
	ReturnErr     error
	mu            sync.Mutex
	lastReq       *llm.ChatRequest
	calls         int
}

// Chat satisfies the package-private chatCaller interface.
func (f *FakeChatCaller) Chat(_ context.Context, req llm.ChatRequest) (*llm.ChatResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	cp := req
	f.lastReq = &cp
	if f.ReturnErr != nil {
		return nil, f.ReturnErr
	}
	return &llm.ChatResponse{Content: f.ReturnContent}, nil
}

// LastReq returns the most recent ChatRequest captured by Chat (or nil if
// Chat was never invoked). Test-only accessor — intentionally a method on
// the public FakeChatCaller, not a public field, so concurrent test
// inspections lock-step the read.
func (f *FakeChatCaller) LastReq() *llm.ChatRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.lastReq
}

// Calls returns the number of times Chat has been invoked. Lets tests
// distinguish "fired" from "did not fire" without depending on goroutine
// timing or the contents of LastReq.
func (f *FakeChatCaller) Calls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}
