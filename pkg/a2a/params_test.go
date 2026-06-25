package a2a_test

import (
	"encoding/json"
	"testing"

	"github.com/f1xgun/onevoice/pkg/a2a"
)

func TestGetIntParam_Float64(t *testing.T) {
	got, err := a2a.GetIntParam(map[string]any{"count": float64(42)}, "count", 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != 42 {
		t.Fatalf("expected 42, got %d", got)
	}
}

func TestGetIntParam_JSONNumber(t *testing.T) {
	got, err := a2a.GetIntParam(map[string]any{"count": json.Number("7")}, "count", 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != 7 {
		t.Fatalf("expected 7, got %d", got)
	}
}

func TestGetIntParam_StringEncodedInt(t *testing.T) {
	got, err := a2a.GetIntParam(map[string]any{"post_id": "4521"}, "post_id", 0)
	if err != nil {
		t.Fatalf("unexpected error for string-encoded int: %v", err)
	}
	if got != 4521 {
		t.Fatalf("expected string \"4521\" to parse to 4521, got %d", got)
	}
}

func TestGetIntParam_StringWithSurroundingSpace(t *testing.T) {
	got, err := a2a.GetIntParam(map[string]any{"post_id": "  88 "}, "post_id", 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != 88 {
		t.Fatalf("expected 88, got %d", got)
	}
}

func TestGetIntParam_StringNonNumericReturnsDefaultAndError(t *testing.T) {
	got, err := a2a.GetIntParam(map[string]any{"post_id": "abc"}, "post_id", 9)
	if err == nil {
		t.Fatal("expected error for non-numeric string")
	}
	if got != 9 {
		t.Fatalf("expected default 9 on parse failure, got %d", got)
	}
}

func TestGetIntParam_AbsentKeyReturnsDefault(t *testing.T) {
	got, err := a2a.GetIntParam(map[string]any{}, "post_id", 5)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != 5 {
		t.Fatalf("expected default 5, got %d", got)
	}
}
