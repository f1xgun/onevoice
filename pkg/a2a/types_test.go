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
