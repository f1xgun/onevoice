package crypto

import "github.com/google/uuid"

// EnvelopeAADForTest exposes envelopeAAD for black-box tests in package crypto_test.
func EnvelopeAADForTest(integrationID uuid.UUID, platform string) []byte {
	return envelopeAAD(integrationID, platform)
}
