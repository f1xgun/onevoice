package agent_test

import (
	"context"
	"encoding/base64"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/a2a"
	"github.com/f1xgun/onevoice/pkg/crypto"
	"github.com/f1xgun/onevoice/pkg/tools"
	"github.com/f1xgun/onevoice/services/agent-yandex-business/internal/agent"
)

// aes256TestKey is a 32-byte AES-256 key shared by the sealed-cookies tests.
const aes256TestKey = "0123456789abcdef0123456789abcdef"

func TestListCompanies_DecryptsSealedCookies(t *testing.T) {
	enc, err := crypto.NewEncryptor([]byte(aes256TestKey))
	require.NoError(t, err)

	cookies := `[{"name":"Session_id","value":"secret-value"}]`
	sealed, err := enc.Encrypt([]byte(cookies))
	require.NoError(t, err)

	pool := &stubPool{browser: &stubBrowser{}}
	h := agent.NewHandler(&fakeTokenFetcher{}, pool, nil).WithPayloadDecryptor(enc)

	resp, err := h.Handle(context.Background(), a2a.ToolRequest{
		TaskID: "list-1",
		Tool:   tools.YandexBusinessListCompanies,
		Args:   map[string]any{"cookies_enc": base64.StdEncoding.EncodeToString(sealed)},
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.True(t, pool.forBusinessCalled)
	assert.Equal(t, cookies, pool.lastCredential, "sealed cookies must be decrypted before hitting the browser")
}

func TestListCompanies_SealedCookiesWithoutKeyFails(t *testing.T) {
	pool := &stubPool{browser: &stubBrowser{}}
	h := agent.NewHandler(&fakeTokenFetcher{}, pool, nil)

	_, err := h.Handle(context.Background(), a2a.ToolRequest{
		TaskID: "list-2",
		Tool:   tools.YandexBusinessListCompanies,
		Args:   map[string]any{"cookies_enc": "AAAA"},
	})
	require.Error(t, err)
	assert.False(t, pool.forBusinessCalled, "must not open a browser when the payload key is missing")
}

func TestListCompanies_PlaintextCookiesStillHonored(t *testing.T) {
	cookies := `[{"name":"Session_id","value":"plain"}]`
	pool := &stubPool{browser: &stubBrowser{}}
	h := agent.NewHandler(&fakeTokenFetcher{}, pool, nil)

	resp, err := h.Handle(context.Background(), a2a.ToolRequest{
		TaskID: "list-3",
		Tool:   tools.YandexBusinessListCompanies,
		Args:   map[string]any{"cookies": cookies},
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.True(t, pool.forBusinessCalled)
	assert.Equal(t, cookies, pool.lastCredential)
}
