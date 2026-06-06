package service

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestYandexSmartCaptcha_OK(t *testing.T) {
	stub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"status":"ok","message":"","host":"localhost"}`))
	}))
	defer stub.Close()

	v := newYandexSmartCaptchaForTest("secret", stub.URL)
	err := v.Verify(context.Background(), "tok", "1.2.3.4")
	assert.NoError(t, err)
}

func TestYandexSmartCaptcha_Failed(t *testing.T) {
	stub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"status":"failed","message":"invalid token"}`))
	}))
	defer stub.Close()

	v := newYandexSmartCaptchaForTest("secret", stub.URL)
	err := v.Verify(context.Background(), "tok", "1.2.3.4")
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrCaptchaInvalid), "expected ErrCaptchaInvalid, got %v", err)
}

func TestYandexSmartCaptcha_NetworkError(t *testing.T) {
	stub := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {}))
	addr := stub.URL
	stub.Close()

	v := newYandexSmartCaptchaForTest("secret", addr)
	err := v.Verify(context.Background(), "tok", "1.2.3.4")
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrCaptchaTransient), "expected ErrCaptchaTransient, got %v", err)
}

func TestYandexSmartCaptcha_BodyShape(t *testing.T) {
	var (
		gotMethod      string
		gotContentType string
		gotForm        url.Values
	)
	stub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotContentType = r.Header.Get("Content-Type")
		body, _ := io.ReadAll(r.Body)
		gotForm, _ = url.ParseQuery(string(body))
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	defer stub.Close()

	v := newYandexSmartCaptchaForTest("the-secret", stub.URL)
	require.NoError(t, v.Verify(context.Background(), "the-token", "9.8.7.6"))

	assert.Equal(t, http.MethodPost, gotMethod)
	assert.Equal(t, "application/x-www-form-urlencoded", gotContentType)
	assert.Equal(t, "the-secret", gotForm.Get("secret"))
	assert.Equal(t, "the-token", gotForm.Get("token"))
	assert.Equal(t, "9.8.7.6", gotForm.Get("ip"))
}

func TestYandexSmartCaptcha_OmitsIPWhenEmpty(t *testing.T) {
	var gotForm url.Values
	stub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		gotForm, _ = url.ParseQuery(string(body))
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	defer stub.Close()

	v := newYandexSmartCaptchaForTest("the-secret", stub.URL)
	require.NoError(t, v.Verify(context.Background(), "the-token", ""))

	_, hasIP := gotForm["ip"]
	assert.False(t, hasIP, "ip field must be omitted when clientIP is empty")
}

func TestYandexSmartCaptcha_GarbageResponse_Transient(t *testing.T) {
	stub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("<html><body>500</body></html>"))
	}))
	defer stub.Close()

	v := newYandexSmartCaptchaForTest("secret", stub.URL)
	err := v.Verify(context.Background(), "tok", "1.2.3.4")
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrCaptchaTransient), "expected ErrCaptchaTransient, got %v", err)
}

func TestNoopSmartCaptcha_AlwaysOK(t *testing.T) {
	v := NewNoopSmartCaptcha()
	assert.NoError(t, v.Verify(context.Background(), "anything", "anywhere"))
	assert.NoError(t, v.Verify(context.Background(), "", ""))
}

// Sanity: the production endpoint constant matches the documented Yandex URL.
// Catches a copy-paste swap to a typo'd hostname during a refactor.
func TestProductionEndpointMatchesYandexDocs(t *testing.T) {
	assert.True(t, strings.HasPrefix(captchaValidateEndpoint, "https://smartcaptcha.yandexcloud.net/"))
}
