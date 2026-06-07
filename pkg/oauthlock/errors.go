package oauthlock

import "errors"

var (
	// ErrLockBusy is returned when pg_try_advisory_xact_lock returns false —
	// another transaction holds the lock for this integration.
	ErrLockBusy = errors.New("oauthlock: lock busy")

	// ErrLockExhausted is returned by RefreshWithRetry when all backoff
	// attempts are exhausted without acquiring the lock.
	ErrLockExhausted = errors.New("oauthlock: retries exhausted")
)
