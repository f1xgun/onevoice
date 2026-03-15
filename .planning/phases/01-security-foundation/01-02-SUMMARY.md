---
plan: "01-02"
status: complete
---
# Plan 01-02: Typed JWT claims with full validation -- Summary

## Tasks Completed
- Task 1-02-01: Created shared `auth/claims.go` package with `AccessTokenClaims`, `RefreshTokenClaims` types, and `TokenIssuer`/`TokenAudience` constants
- Task 1-02-02: Updated token generation in `user.go` to use shared auth types, added `Issuer` and `Audience` registered claims to both access and refresh tokens, added `WithValidMethods`/`WithIssuer`/`WithAudience` parser options to refresh token parsing
- Task 1-02-03: Migrated auth middleware from `jwt.MapClaims` to typed `auth.AccessTokenClaims` with full validation (issuer, audience, signing method), replaced raw error leakage with typed error codes (`token_expired`, `token_invalid`)

## Key Files
### Created
- services/api/internal/auth/claims.go

### Modified
- services/api/internal/service/user.go
- services/api/internal/middleware/auth.go
- services/api/internal/service/user_test.go
- services/api/internal/middleware/auth_test.go

## Self-Check
- [x] Claims types live in `services/api/internal/auth/claims.go`, shared by middleware and service
- [x] Access tokens include `Issuer: "onevoice-api"` and `Audience: ["onevoice"]` registered claims
- [x] Refresh tokens include `Issuer: "onevoice-api"` and `Audience: ["onevoice"]` registered claims
- [x] Auth middleware uses `jwt.ParseWithClaims` with `&auth.AccessTokenClaims{}`
- [x] Auth middleware validates `ValidMethods`, `Issuer`, and `Audience` via parser options
- [x] Auth middleware returns typed error codes (`token_expired`, `token_invalid`) -- never raw jwt error strings
- [x] No `jwt.MapClaims` usage remains in production code
- [x] `cd services/api && GOWORK=off go build ./...` succeeds
- [x] `cd services/api && GOWORK=off go test -race ./internal/middleware/... ./internal/service/...` passes

## Deviations
- Tests in both `user_test.go` and `auth_test.go` required updates to use typed claims with issuer/audience (tests were not explicitly mentioned in the plan but were necessary for correctness)
- `TestAuth_MissingClaims` and `TestAuth_InvalidUserIDFormat` now check for `token_invalid` instead of the previous specific error messages, since MapClaims tokens without issuer/audience fail earlier in validation
