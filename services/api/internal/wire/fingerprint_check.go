package wire

import (
	"context"
	"fmt"
	"log"

	"github.com/f1xgun/onevoice/pkg/domain"
)

// RunFingerprintCheck queries the integration repository for rows whose
// encryption_key_fingerprint does not match currentFP. Rows with a NULL
// fingerprint (legacy rows pre-rekey) are excluded from the count.
// If the mismatch count is ≥ 1 the process exits immediately via log.Fatalf
// — the operator must either restore the correct TOKEN_ENCRYPTION_KMS_KEY_ID
// or run cmd/rekey before the API will accept traffic.
func RunFingerprintCheck(ctx context.Context, repo domain.IntegrationRepository, currentFP string) error {
	mismatch, err := repo.CountIntegrationsWithDifferentFingerprint(ctx, currentFP)
	if err != nil {
		return fmt.Errorf("wire: fingerprint check: %w", err)
	}
	if mismatch > 0 {
		log.Fatalf("boot refusing to start: %d rows encrypted with a different KMS key (fingerprint mismatch). Run cmd/rekey or restore correct TOKEN_ENCRYPTION_KMS_KEY_ID. current_fingerprint=%s", mismatch, currentFP)
	}
	return nil
}
