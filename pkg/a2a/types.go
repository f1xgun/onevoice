package a2a

import "errors"

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

// CodedError annotates an error with a typed classifier code so the FE
// can render a localized explanation without parsing free-text. The locked
// enum is: integration_token_invalid, rate_limit_exceeded, transient,
// channel_not_found, media_too_large. CodedError composes with
// NonRetryableError — wrap NonRetryableError inside CodedError and
// errors.Is(out, &NonRetryableError{}) continues to return true so
// existing withRetry guards in agentbase remain effective.
type CodedError struct {
	Code string
	Err  error
}

func (e *CodedError) Error() string { return e.Err.Error() }
func (e *CodedError) Unwrap() error { return e.Err }

// NewCodedError wraps err with the given classifier code.
func NewCodedError(code string, err error) *CodedError {
	return &CodedError{Code: code, Err: err}
}

// CodeOf returns the Code from the first *CodedError in the error chain,
// or "" if no CodedError is found.
func CodeOf(err error) string {
	var ce *CodedError
	if errors.As(err, &ce) {
		return ce.Code
	}
	return ""
}
