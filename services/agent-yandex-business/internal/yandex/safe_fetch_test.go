package yandex

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestIsDisallowedIP_ThisHostBlock(t *testing.T) {
	tests := []struct {
		name string
		ip   string
		want bool
	}{
		{name: "0.0.0.0 unspecified", ip: "0.0.0.0", want: true},
		{name: "0.0.0.1 this-host loopback-routed", ip: "0.0.0.1", want: true},
		{name: "0.10.0.1 within 0.0.0.0/8", ip: "0.10.0.1", want: true},
		{name: "0.255.255.255 top of 0.0.0.0/8", ip: "0.255.255.255", want: true},
		{name: "loopback 127.0.0.1", ip: "127.0.0.1", want: true},
		{name: "metadata 169.254.169.254", ip: "169.254.169.254", want: true},
		{name: "private 10.0.0.5", ip: "10.0.0.5", want: true},
		{name: "cgnat 100.64.0.1", ip: "100.64.0.1", want: true},
		{name: "public 93.184.216.34", ip: "93.184.216.34", want: false},
		{name: "public 1.1.1.1", ip: "1.1.1.1", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ip := net.ParseIP(tt.ip)
			if ip == nil {
				t.Fatalf("ParseIP(%q) returned nil", tt.ip)
			}
			if got := isDisallowedIP(ip); got != tt.want {
				t.Fatalf("isDisallowedIP(%s) = %v, want %v", tt.ip, got, tt.want)
			}
		})
	}
}

func TestValidatePhotoURL(t *testing.T) {
	tests := []struct {
		name    string
		rawURL  string
		wantErr bool
	}{
		{name: "public https host", rawURL: "https://example.com/photo.jpg", wantErr: false},
		{name: "public ip literal", rawURL: "https://93.184.216.34/photo.jpg", wantErr: false},
		{name: "http scheme rejected", rawURL: "http://example.com/photo.jpg", wantErr: true},
		{name: "ftp scheme rejected", rawURL: "ftp://example.com/photo.jpg", wantErr: true},
		{name: "empty host rejected", rawURL: "https:///photo.jpg", wantErr: true},
		{name: "loopback ipv4 rejected", rawURL: "https://127.0.0.1/photo.jpg", wantErr: true},
		{name: "loopback ipv6 rejected", rawURL: "https://[::1]/photo.jpg", wantErr: true},
		{name: "metadata link-local rejected", rawURL: "https://169.254.169.254/latest/meta-data", wantErr: true},
		{name: "private 10/8 rejected", rawURL: "https://10.0.0.5/photo.jpg", wantErr: true},
		{name: "private 172.16/12 rejected", rawURL: "https://172.16.0.1/photo.jpg", wantErr: true},
		{name: "private 192.168/16 rejected", rawURL: "https://192.168.1.1/photo.jpg", wantErr: true},
		{name: "cgnat 100.64/10 rejected", rawURL: "https://100.64.0.1/photo.jpg", wantErr: true},
		{name: "this-host 0.0.0.1 rejected", rawURL: "https://0.0.0.1/photo.jpg", wantErr: true},
		{name: "this-host 0.10.0.1 rejected", rawURL: "https://0.10.0.1/photo.jpg", wantErr: true},
		{name: "unique local ipv6 rejected", rawURL: "https://[fc00::1]/photo.jpg", wantErr: true},
		{name: "link-local ipv6 rejected", rawURL: "https://[fe80::1]/photo.jpg", wantErr: true},
		{name: "unparseable url rejected", rawURL: "https://exa mple.com/photo.jpg", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validatePhotoURL(tt.rawURL)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("validatePhotoURL(%q) = nil, want error", tt.rawURL)
				}
				if !errors.Is(err, ErrUnsafeURL) {
					t.Fatalf("validatePhotoURL(%q) error = %v, want ErrUnsafeURL", tt.rawURL, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("validatePhotoURL(%q) = %v, want nil", tt.rawURL, err)
			}
		})
	}
}

func TestFetchPhotoRejectsNonHTTPS(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("data"))
	}))
	defer srv.Close()

	_, err := fetchPhoto(context.Background(), srv.URL)
	if err == nil {
		t.Fatal("expected fetchPhoto to reject plain-http test server URL")
	}
	if !errors.Is(err, ErrUnsafeURL) {
		t.Fatalf("expected ErrUnsafeURL, got: %v", err)
	}
}

func TestFetchPhotoCapsBodySize(t *testing.T) {
	oversized := bytes.Repeat([]byte("a"), maxPhotoBytes+1024)
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(oversized)
	}))
	defer srv.Close()

	client := srv.Client()
	resp, err := client.Get(srv.URL)
	if err != nil {
		t.Fatalf("get oversized body: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxPhotoBytes+1))
	if err != nil {
		t.Fatalf("read limited body: %v", err)
	}
	if int64(len(body)) <= maxPhotoBytes {
		t.Fatalf("LimitReader read %d bytes, expected it to exceed cap %d so the size guard trips", len(body), maxPhotoBytes)
	}
}

func TestFetchPhotoTruncatesExactlyAtCap(t *testing.T) {
	payload := strings.Repeat("x", 32)
	const cap64 = 8
	body, err := io.ReadAll(io.LimitReader(strings.NewReader(payload), cap64))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(body) != cap64 {
		t.Fatalf("LimitReader returned %d bytes, want %d", len(body), cap64)
	}
}
