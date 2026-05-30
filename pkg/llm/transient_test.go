package llm

import (
	"errors"
	"fmt"
	"testing"

	anthropic "github.com/anthropics/anthropic-sdk-go"
	openai "github.com/sashabaranov/go-openai"
)

// fakeNetErr satisfies net.Error so the transient detector exercises the
// Timeout() / Temporary() branch without depending on real network I/O.
type fakeNetErr struct {
	msg       string
	timeout   bool
	temporary bool
}

func (e *fakeNetErr) Error() string   { return e.msg }
func (e *fakeNetErr) Timeout() bool   { return e.timeout }
func (e *fakeNetErr) Temporary() bool { return e.temporary }

func TestIsTransient_NilError(t *testing.T) {
	if isTransientLLMError(nil) {
		t.Fatal("nil error must not be transient")
	}
}

func TestIsTransient_NetTimeoutError(t *testing.T) {
	netErr := &fakeNetErr{msg: "dial tcp: i/o timeout", timeout: true}
	wrapped := fmt.Errorf("openai chat: %w", netErr)
	if !isTransientLLMError(wrapped) {
		t.Fatal("net.Error Timeout() must be detected as transient")
	}
}

func TestIsTransient_NetTemporaryError(t *testing.T) {
	netErr := &fakeNetErr{msg: "connection reset", temporary: true}
	wrapped := fmt.Errorf("anthropic chat: %w", netErr)
	if !isTransientLLMError(wrapped) {
		t.Fatal("net.Error Temporary() must be detected as transient")
	}
}

func TestIsTransient_OpenAI429(t *testing.T) {
	apiErr := &openai.APIError{HTTPStatusCode: 429, Message: "rate limited"}
	wrapped := fmt.Errorf("openai chat: %w", apiErr)
	if !isTransientLLMError(wrapped) {
		t.Fatal("openai 429 must be transient")
	}
}

func TestIsTransient_OpenAI500(t *testing.T) {
	apiErr := &openai.APIError{HTTPStatusCode: 500, Message: "internal"}
	if !isTransientLLMError(fmt.Errorf("openai chat: %w", apiErr)) {
		t.Fatal("openai 500 must be transient")
	}
}

func TestIsTransient_OpenAI503(t *testing.T) {
	apiErr := &openai.APIError{HTTPStatusCode: 503, Message: "unavailable"}
	if !isTransientLLMError(fmt.Errorf("openai chat: %w", apiErr)) {
		t.Fatal("openai 503 must be transient")
	}
}

func TestIsTransient_OpenAI599(t *testing.T) {
	apiErr := &openai.APIError{HTTPStatusCode: 599, Message: "edge"}
	if !isTransientLLMError(fmt.Errorf("openai chat: %w", apiErr)) {
		t.Fatal("openai 599 must be transient (5xx range)")
	}
}

func TestIsTransient_OpenAI400(t *testing.T) {
	apiErr := &openai.APIError{HTTPStatusCode: 400, Message: "bad request"}
	if isTransientLLMError(fmt.Errorf("openai chat: %w", apiErr)) {
		t.Fatal("openai 400 must NOT be transient")
	}
}

func TestIsTransient_OpenAI401(t *testing.T) {
	apiErr := &openai.APIError{HTTPStatusCode: 401, Message: "unauthorized"}
	if isTransientLLMError(fmt.Errorf("openai chat: %w", apiErr)) {
		t.Fatal("openai 401 must NOT be transient")
	}
}

func TestIsTransient_OpenAI404(t *testing.T) {
	apiErr := &openai.APIError{HTTPStatusCode: 404, Message: "not found"}
	if isTransientLLMError(fmt.Errorf("openai chat: %w", apiErr)) {
		t.Fatal("openai 404 must NOT be transient")
	}
}

func TestIsTransient_Anthropic429(t *testing.T) {
	apiErr := &anthropic.Error{StatusCode: 429}
	if !isTransientLLMError(fmt.Errorf("anthropic chat: %w", apiErr)) {
		t.Fatal("anthropic 429 must be transient")
	}
}

func TestIsTransient_Anthropic503(t *testing.T) {
	apiErr := &anthropic.Error{StatusCode: 503}
	if !isTransientLLMError(fmt.Errorf("anthropic chat: %w", apiErr)) {
		t.Fatal("anthropic 503 must be transient")
	}
}

func TestIsTransient_Anthropic400(t *testing.T) {
	apiErr := &anthropic.Error{StatusCode: 400}
	if isTransientLLMError(fmt.Errorf("anthropic chat: %w", apiErr)) {
		t.Fatal("anthropic 400 must NOT be transient")
	}
}

func TestIsTransient_SubstringFallback_429(t *testing.T) {
	err := errors.New("upstream returned 429 too many requests")
	if !isTransientLLMError(err) {
		t.Fatal("plain error with '429' substring must be transient (fallback)")
	}
}

func TestIsTransient_SubstringFallback_502(t *testing.T) {
	err := errors.New("bad gateway 502")
	if !isTransientLLMError(err) {
		t.Fatal("plain error with '502' substring must be transient")
	}
}

func TestIsTransient_SubstringFallback_503(t *testing.T) {
	err := errors.New("service unavailable 503")
	if !isTransientLLMError(err) {
		t.Fatal("plain error with '503' substring must be transient")
	}
}

func TestIsTransient_SubstringFallback_504(t *testing.T) {
	err := errors.New("gateway timeout 504")
	if !isTransientLLMError(err) {
		t.Fatal("plain error with '504' substring must be transient")
	}
}

func TestIsTransient_PlainNonTransient(t *testing.T) {
	err := errors.New("context canceled")
	if isTransientLLMError(err) {
		t.Fatal("plain non-transient error must not be transient")
	}
}

func TestIsTransient_PlainBadRequest(t *testing.T) {
	err := errors.New("missing required field")
	if isTransientLLMError(err) {
		t.Fatal("plain validation error must not be transient")
	}
}
