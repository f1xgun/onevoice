package service

import (
	"testing"
)

func TestNewUserService_ShortSecret_ReturnsError(t *testing.T) {
	svc, err := NewUserService(nil, nil, "short")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if svc != nil {
		t.Fatal("expected nil service")
	}
}

func TestNewUserService_ValidSecret_ReturnsService(t *testing.T) {
	svc, err := NewUserService(nil, nil, "a]32-byte-or-longer-secret-key!!")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if svc == nil {
		t.Fatal("expected non-nil service")
	}
}
