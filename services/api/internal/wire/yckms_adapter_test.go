package wire

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/crypto"
)

// errKMS is a stub crypto.KMSEncrypter whose Encrypt always returns an error.
type errKMS struct{ err error }

func (e *errKMS) Encrypt(_ context.Context, _, _ []byte) ([]byte, string, error) {
	return nil, "", e.err
}
func (e *errKMS) Decrypt(_ context.Context, _, _ []byte) ([]byte, string, error) {
	return nil, "", e.err
}

// okKMS is a stub crypto.KMSEncrypter whose Encrypt always succeeds.
type okKMS struct{}

func (o *okKMS) Encrypt(_ context.Context, plaintext, _ []byte) ([]byte, string, error) {
	return plaintext, "1", nil
}
func (o *okKMS) Decrypt(_ context.Context, ciphertext, _ []byte) ([]byte, string, error) {
	return ciphertext, "1", nil
}

var _ crypto.KMSEncrypter = (*errKMS)(nil)
var _ crypto.KMSEncrypter = (*okKMS)(nil)

func TestNewKMSClient_RequiresSA(t *testing.T) {
	_, err := NewKMSClient(context.Background(), nil, "some-key-id", "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "empty YC SA credentials")
}

func TestNewKMSClient_RequiresKeyID(t *testing.T) {
	_, err := NewKMSClient(context.Background(), []byte(`{"id":"x"}`), "", "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "empty KMS key id")
}

func TestNewKMSClient_InvalidSAJSON(t *testing.T) {
	_, err := NewKMSClient(context.Background(), []byte(`not-valid-json`), "some-key-id", "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "parse sa key")
}

func TestNewKMSClient_DualVersionsCSVIgnored(t *testing.T) {
	// dualVersionsCSV is accepted for API compatibility but ignored; passing
	// many version strings no longer produces an error.
	validSAJSON := []byte(`{"id":"x","service_account_id":"sa","created_at":"2024-01-01T00:00:00Z","key_algorithm":"RSA_2048","public_key":"pk","private_key":"priv"}`)
	_, err := NewKMSClient(context.Background(), validSAJSON, "some-key-id", "v1,v2,v3,v4,v5,v6")
	// Expect SA credential parse error (not a cap-exceeded error).
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "cap exceeded")
}

func TestKMSSelfTestFailFast(t *testing.T) {
	synthetic := errors.New("kms: connection refused")
	err := kmsSelfTest(context.Background(), &errKMS{err: synthetic})
	require.Error(t, err)
	assert.ErrorIs(t, err, synthetic)
	assert.Contains(t, err.Error(), "kms self-test failed")
}

func TestKMSSelfTestSucceeds(t *testing.T) {
	err := kmsSelfTest(context.Background(), &okKMS{})
	require.NoError(t, err)
}
