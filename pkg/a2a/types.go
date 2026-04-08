package a2a

// NonRetryableError marks an error as permanent — withRetry must not retry it.
type NonRetryableError struct {
	Err error
}

func (e *NonRetryableError) Error() string { return e.Err.Error() }
func (e *NonRetryableError) Unwrap() error { return e.Err }
func (e *NonRetryableError) Is(target error) bool {
	_, ok := target.(*NonRetryableError)
	return ok
}

// NewNonRetryableError wraps err as a permanent failure that should not be retried.
func NewNonRetryableError(err error) *NonRetryableError {
	return &NonRetryableError{Err: err}
}
