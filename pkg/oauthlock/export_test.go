package oauthlock

import "github.com/google/uuid"

// LockKeyForTest exposes lockKey for determinism tests.
func LockKeyForTest(id uuid.UUID) int64 {
	return lockKey(id)
}
