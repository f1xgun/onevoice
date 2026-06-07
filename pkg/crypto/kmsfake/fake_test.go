package kmsfake

import (
	"bytes"
	"context"
	"testing"
)

func TestRoundTrip(t *testing.T) {
	f := New()
	ct, ver, err := f.Encrypt(context.Background(), []byte("hello"), []byte("aad-1"))
	if err != nil {
		t.Fatal(err)
	}
	if ver != "1" {
		t.Fatalf("want version 1, got %q", ver)
	}
	pt, ver2, err := f.Decrypt(context.Background(), ct, []byte("aad-1"))
	if err != nil {
		t.Fatal(err)
	}
	if ver2 != "1" {
		t.Fatalf("decrypt version mismatch: want %q got %q", "1", ver2)
	}
	if !bytes.Equal(pt, []byte("hello")) {
		t.Fatalf("round-trip mismatch: got %q", pt)
	}
}

func TestAADMismatch(t *testing.T) {
	f := New()
	ct, _, _ := f.Encrypt(context.Background(), []byte("x"), []byte("aad-A"))
	if _, _, err := f.Decrypt(context.Background(), ct, []byte("aad-B")); err == nil {
		t.Fatal("expected aad mismatch error")
	}
}

func TestVersionRotation(t *testing.T) {
	f := New()
	f.RotateToVersion(2)
	ct, ver, _ := f.Encrypt(context.Background(), []byte("x"), []byte("a"))
	if ver != "2" {
		t.Fatalf("want version 2, got %q", ver)
	}
	if _, _, err := f.Decrypt(context.Background(), ct, []byte("a")); err != nil {
		t.Fatal(err)
	}
}

func TestIntegrationAAD(t *testing.T) {
	f := New()
	integrationID := "550e8400-e29b-41d4-a716-446655440000"
	platform := "telegram"
	aad := []byte(integrationID + "|" + platform)

	ct, _, err := f.Encrypt(context.Background(), []byte("secret-token"), aad)
	if err != nil {
		t.Fatal(err)
	}
	pt, _, err := f.Decrypt(context.Background(), ct, aad)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(pt, []byte("secret-token")) {
		t.Fatalf("integration aad round-trip failed: got %q", pt)
	}

	if _, _, err := f.Decrypt(context.Background(), ct, []byte("wrong-id|telegram")); err == nil {
		t.Fatal("expected aad mismatch on wrong integrationID")
	}
}

func TestUnknownVersion(t *testing.T) {
	f := New()
	f.RotateToVersion(2)
	ct, _, err := f.Encrypt(context.Background(), []byte("x"), []byte("a"))
	if err != nil {
		t.Fatal(err)
	}
	f2 := New()
	if _, _, err := f2.Decrypt(context.Background(), ct, []byte("a")); err == nil {
		t.Fatal("expected unknown version error for version 2 on fresh encrypter at version 1")
	}
}
