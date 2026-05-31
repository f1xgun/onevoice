package a2a_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/f1xgun/onevoice/pkg/a2a"
)

func TestNonRetryableError_Is(t *testing.T) {
	someErr := errors.New("permanent failure")
	nre := a2a.NewNonRetryableError(someErr)

	if !errors.Is(nre, &a2a.NonRetryableError{}) {
		t.Fatal("expected errors.Is to return true for NonRetryableError")
	}
}

func TestNonRetryableError_IsNegative(t *testing.T) {
	normalErr := fmt.Errorf("normal error")

	if errors.Is(normalErr, &a2a.NonRetryableError{}) {
		t.Fatal("expected errors.Is to return false for a normal error")
	}
}

func TestNonRetryableError_Unwrap(t *testing.T) {
	someErr := errors.New("inner error")
	nre := a2a.NewNonRetryableError(someErr)

	unwrapped := errors.Unwrap(nre)
	if unwrapped != someErr {
		t.Fatalf("expected unwrapped error to be %v, got %v", someErr, unwrapped)
	}
}

func TestNonRetryableError_ErrorMessage(t *testing.T) {
	someErr := errors.New("something broke")
	nre := a2a.NewNonRetryableError(someErr)

	if nre.Error() != "something broke" {
		t.Fatalf("expected error message %q, got %q", "something broke", nre.Error())
	}
}

func TestNonRetryableError_IsWrapped(t *testing.T) {
	someErr := errors.New("root cause")
	nre := a2a.NewNonRetryableError(someErr)
	wrapped := fmt.Errorf("wrap: %w", nre)

	if !errors.Is(wrapped, &a2a.NonRetryableError{}) {
		t.Fatal("expected errors.Is to return true through wrapping chain")
	}
}

func TestCodedError_Error_DelegatesToInner(t *testing.T) {
	inner := errors.New("something broke")
	ce := a2a.NewCodedError("transient", inner)
	if ce.Error() != "something broke" {
		t.Fatalf("expected delegated error message, got %q", ce.Error())
	}
}

func TestCodedError_Unwrap_ReturnsInner(t *testing.T) {
	inner := errors.New("inner cause")
	ce := a2a.NewCodedError("integration_token_invalid", inner)
	if got := errors.Unwrap(ce); got != inner {
		t.Fatalf("expected unwrap to return inner, got %v", got)
	}
}

func TestCodedError_ErrorsIs_FindsNonRetryable(t *testing.T) {
	inner := errors.New("token revoked")
	nre := a2a.NewNonRetryableError(inner)
	wrapped := a2a.NewCodedError("integration_token_invalid", nre)

	if !errors.Is(wrapped, &a2a.NonRetryableError{}) {
		t.Fatal("expected errors.Is(CodedError(NonRetryableError(...))) to return true — composition guarantee")
	}
}

func TestCodeOf_FindsCodeInChain(t *testing.T) {
	inner := errors.New("rate limit hit")
	ce := a2a.NewCodedError("rate_limit_exceeded", inner)
	wrapped := fmt.Errorf("dispatch: %w", ce)

	if got := a2a.CodeOf(wrapped); got != "rate_limit_exceeded" {
		t.Fatalf("expected code rate_limit_exceeded through wrap chain, got %q", got)
	}
}

func TestCodeOf_NoCode_ReturnsEmpty(t *testing.T) {
	plain := errors.New("plain error")
	if got := a2a.CodeOf(plain); got != "" {
		t.Fatalf("expected empty code for plain error, got %q", got)
	}
	if got := a2a.CodeOf(nil); got != "" {
		t.Fatalf("expected empty code for nil, got %q", got)
	}
}
