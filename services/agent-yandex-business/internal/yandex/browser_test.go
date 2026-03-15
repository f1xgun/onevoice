package yandex

import (
	"context"
	"errors"
	"testing"

	"github.com/f1xgun/onevoice/pkg/a2a"
)

func TestWithRetry_NonRetryableError_StopsImmediately(t *testing.T) {
	callCount := 0
	permErr := a2a.NewNonRetryableError(errors.New("permanent"))

	err := withRetry(context.Background(), 3, func() error {
		callCount++
		return permErr
	})

	if callCount != 1 {
		t.Fatalf("expected fn to be called 1 time, got %d", callCount)
	}
	if !errors.Is(err, &a2a.NonRetryableError{}) {
		t.Fatalf("expected NonRetryableError, got %v", err)
	}
}

func TestWithRetry_TransientError_RetriesAll(t *testing.T) {
	callCount := 0
	transientErr := errors.New("transient")

	err := withRetry(context.Background(), 3, func() error {
		callCount++
		return transientErr
	})

	if callCount != 3 {
		t.Fatalf("expected fn to be called 3 times, got %d", callCount)
	}
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestWithRetry_SuccessOnSecondAttempt(t *testing.T) {
	callCount := 0

	err := withRetry(context.Background(), 3, func() error {
		callCount++
		if callCount == 1 {
			return errors.New("transient")
		}
		return nil
	})

	if callCount != 2 {
		t.Fatalf("expected fn to be called 2 times, got %d", callCount)
	}
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
}

func TestWithRetry_ContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	callCount := 0
	err := withRetry(ctx, 3, func() error {
		callCount++
		return errors.New("should not reach")
	})

	if callCount != 0 {
		t.Fatalf("expected fn to not be called, got %d calls", callCount)
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled error, got %v", err)
	}
}
