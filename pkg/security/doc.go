// Package security provides shared security primitives for OneVoice services.
//
// This package contains a reusable PII-detection module (pii.go) with named
// regex classes, Luhn validation for credit-card candidates, and Russian-aware
// false-positive guards: passport / INN regexes require explicit Cyrillic
// prefix anchors so legitimate numeric titles like "Заказ 12345" or
// "Заявка 7654321098" do not match.
//
// Phase 18 auto-titler is the primary consumer:
//   - RedactPII pre-scrubs user/assistant messages before they reach the cheap
//     LLM endpoint (D-14 defense-in-depth).
//   - ContainsPIIClass post-scans the generated title and returns the matched
//     class name so callers can log a regex_class field WITHOUT exposing the
//     matched substring (D-16).
//
// Phase 19 search-query logging is the planned secondary consumer (D-15).
package security
