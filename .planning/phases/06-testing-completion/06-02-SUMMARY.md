---
plan: "06-02"
title: "Health check endpoint tests for all services"
status: complete
completed: "2026-03-20"
---

## Summary

Expanded `pkg/health/health_test.go` from 4 tests to 10 tests, covering all edge cases for the health check package.

## Tests Added

| Test | Scenario | Status Code | Key Assertion |
|------|----------|-------------|---------------|
| `TestReadyHandler_AllFailing` | Both checks return errors | 503 | Both error messages in `checks` map |
| `TestReadyHandler_ContextTimeout` | Check blocks on `ctx.Done()` | 503 | Context error reported |
| `TestLiveHandler_ContentType` | Content-Type header | 200 | `application/json` |
| `TestReadyHandler_ContentType` | Content-Type header | 200 | `application/json` |
| `TestAddCheck_ConcurrentSafety` | 10 goroutines call `AddCheck` | 200 | All 10 checks present, `-race` clean |
| `TestReadyHandler_MixedResults` | 2 healthy + 1 failing | 503 | Per-check detail correct |

## Verification

- `cd pkg && GOWORK=off go test -race -v ./health/...` — all 10 tests PASS
- No race conditions detected

## Files Modified

- `pkg/health/health_test.go` — added 6 new test functions (+192 lines)
