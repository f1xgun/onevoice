// Package wire owns the API service's startup wiring.
package wire

import (
	"context"
	"errors"
	"fmt"

	kms "github.com/yandex-cloud/go-genproto/yandex/cloud/kms/v1"
	ycsdk "github.com/yandex-cloud/go-sdk"
	"github.com/yandex-cloud/go-sdk/iamkey"

	"github.com/f1xgun/onevoice/pkg/crypto"
)

type yckmsClient struct {
	sdk   *ycsdk.SDK
	keyID string
}

// NewKMSClient constructs a crypto.KMSEncrypter backed by Yandex Cloud KMS.
// saJSONBytes must contain a valid Yandex Cloud Service Account JSON key.
// keyID must be a non-empty KMS symmetric key resource ID.
// dualVersionsCSV is accepted for API compatibility but is intentionally
// ignored: the YC KMS server selects the correct key version from the
// ciphertext header automatically, so no version routing is needed at the
// SDK call site.
func NewKMSClient(ctx context.Context, saJSONBytes []byte, keyID, dualVersionsCSV string) (crypto.KMSEncrypter, error) {
	if len(saJSONBytes) == 0 {
		return nil, errors.New("wire: empty YC SA credentials")
	}
	if keyID == "" {
		return nil, errors.New("wire: empty KMS key id")
	}

	key, err := iamkey.ReadFromJSONBytes(saJSONBytes)
	if err != nil {
		return nil, fmt.Errorf("wire: parse sa key: %w", err)
	}
	creds, err := ycsdk.ServiceAccountKey(key)
	if err != nil {
		return nil, fmt.Errorf("wire: sa creds: %w", err)
	}

	bctx, cancel := context.WithTimeout(ctx, startupTimeout)
	defer cancel()
	sdk, err := ycsdk.Build(bctx, ycsdk.Config{Credentials: creds})
	if err != nil {
		return nil, fmt.Errorf("wire: ycsdk build: %w", err)
	}

	return &yckmsClient{sdk: sdk, keyID: keyID}, nil
}

// Encrypt wraps plaintext using the KMS symmetric key. aad is passed as
// AadContext to both the KMS layer and (separately) the AES-GCM layer in
// pkg/crypto/envelope.go. Returns ciphertext, version ID string, and error.
func (c *yckmsClient) Encrypt(ctx context.Context, plaintext, aad []byte) ([]byte, string, error) {
	resp, err := c.sdk.KMSCrypto().SymmetricCrypto().Encrypt(ctx, &kms.SymmetricEncryptRequest{
		KeyId:      c.keyID,
		Plaintext:  plaintext,
		AadContext: aad,
	})
	if err != nil {
		return nil, "", fmt.Errorf("wire: kms encrypt: %w", err)
	}
	return resp.GetCiphertext(), resp.GetVersionId(), nil
}

// Decrypt unwraps ciphertext using the KMS symmetric key. The KMS server
// selects the correct key version from the ciphertext header automatically —
// no version routing is required at the SDK call site.
func (c *yckmsClient) Decrypt(ctx context.Context, ciphertext, aad []byte) ([]byte, string, error) {
	resp, err := c.sdk.KMSCrypto().SymmetricCrypto().Decrypt(ctx, &kms.SymmetricDecryptRequest{
		KeyId:      c.keyID,
		Ciphertext: ciphertext,
		AadContext: aad,
	})
	if err != nil {
		return nil, "", fmt.Errorf("wire: kms decrypt: %w", err)
	}
	return resp.GetPlaintext(), resp.GetVersionId(), nil
}
