---
phase: 10
slug: oauth-token-infrastructure-agent-scaffold
status: draft
nyquist_compliant: false
wave_0_complete: false
created: 2026-04-08
---

# Phase 10 — Validation Strategy

> Per-phase validation contract for feedback sampling during execution.

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | Go testing + testify v1.11.1 |
| **Config file** | None — Go standard `go test` |
| **Quick run command** | `cd services/agent-google-business && go test -race ./...` |
| **Full suite command** | `make test-all` |

---

## Sampling Rate

- **After every task commit:** `cd services/api && go test -race ./... && cd ../../services/agent-google-business && go test -race ./...`
- **After every wave merge:** `make test-all`
- **Phase gate:** Full suite green before `/gsd:verify-work`

---

## Phase Requirements → Test Map

| Req ID | Behavior | Test Type | Automated Command | File Exists? |
|--------|----------|-----------|-------------------|-------------|
| INFRA-01 | Google OAuth flow: auth URL generation, callback token exchange, state validation | unit | `cd services/api && go test -race -run TestGoogleOAuth ./internal/handler/` | Wave 0 |
| INFRA-02 | Account/location discovery after OAuth | unit | `cd services/api && go test -race -run TestGoogleLocations ./internal/handler/` | Wave 0 |
| INFRA-03 | Token refresh in GetDecryptedToken | unit | `cd services/api && go test -race -run TestGetDecryptedToken_GoogleRefresh ./internal/service/` | Wave 0 |
| INTEG-01 | Agent starts, connects NATS, responds to tasks.google_business | unit | `cd services/agent-google-business && go test -race ./...` | Wave 0 |

---

## Wave 0 Gaps

- [ ] `services/agent-google-business/internal/agent/handler_test.go` — covers INTEG-01
- [ ] `services/agent-google-business/internal/gbp/client_test.go` — covers GBP client basics
- [ ] `services/api/internal/handler/oauth_test.go` — add Google OAuth test cases (file exists, needs Google tests)
- [ ] `services/api/internal/service/integration_test.go` — add token refresh test cases (file may exist)
