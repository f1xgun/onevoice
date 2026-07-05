package imagegen

import (
	"errors"
	"fmt"

	openai "github.com/sashabaranov/go-openai"
)

// ErrInvalidParam marks a request parameter (size or style) that is not in the
// provider's allow-set. It is caught BEFORE any paid provider call so an
// undefined value never reaches the API — where it would 4xx and/or bill an
// undefined cost. The same value will always be rejected, so it is not
// retryable without changing the value.
var ErrInvalidParam = errors.New("imagegen: unsupported parameter value")

// allowedSizes is the set of DALL·E 3 image sizes the tool accepts. The
// generator only ever sends these to the provider; validating up front turns an
// LLM-hallucinated size into a clean tool error instead of a wasted paid call.
var allowedSizes = map[string]struct{}{
	openai.CreateImageSize1024x1024: {},
	openai.CreateImageSize1792x1024: {},
	openai.CreateImageSize1024x1792: {},
}

// allowedStyles is the set of DALL·E 3 style hints the tool accepts.
var allowedStyles = map[string]struct{}{
	openai.CreateImageStyleVivid:   {},
	openai.CreateImageStyleNatural: {},
}

// ValidateParams rejects a size or style that is not in the provider allow-set.
// An empty value is allowed for either — it means "use the generator's
// configured default" and is never forwarded verbatim. Callers invoke this
// before Generate so an undefined parameter is refused with a user-safe error
// and no paid provider round-trip.
func ValidateParams(size, style string) error {
	if size != "" {
		if _, ok := allowedSizes[size]; !ok {
			return fmt.Errorf("%w: size %q (allowed: 1024x1024, 1792x1024, 1024x1792)", ErrInvalidParam, size)
		}
	}
	if style != "" {
		if _, ok := allowedStyles[style]; !ok {
			return fmt.Errorf("%w: style %q (allowed: vivid, natural)", ErrInvalidParam, style)
		}
	}
	return nil
}
