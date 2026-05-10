# pkg/agentbase

Shared base utilities consumed by all four platform agents
(`services/agent-telegram`, `services/agent-vk`, `services/agent-yandex-business`,
`services/agent-google-business`).

Extracted from previous 4× duplications across `services/agent-*/cmd/main.go`
and `internal/agent/handler.go`. The package is interface-first; per-platform
behaviour (tool routing, error keywords) stays in the agent package and is
plugged in via the constructors.

## Public API

| Symbol | Purpose | Consumed by |
|---|---|---|
| `TokenInfo` struct (`AccessToken`, `UserToken`, `ExternalID`) | Canonical credentials shape resolved by API's `/internal/v1/tokens` | All 4 agents |
| `TokenResolver` interface + `NewTokenResolver(*tokenclient.Client)` | Fetch credentials for `(businessID, platform, externalID)`; falls back to first active integration when externalID empty | All 4 agents (`cmd/main.go`) |
| `Dispatcher` interface + `NewDispatcher(*hitldedupe.DedupeClient, ErrorClassifier)` | Wraps the HITL dedupe gate around per-agent tool routing (4-step sequence: dedupeGate → exec → classify → store) | All 4 agents (`internal/agent/handler.go`) |
| `ErrorClassifier` interface + `FuncClassifier` adapter | Per-platform permanent-error detection — each agent supplies its own keyword list (telegram HTTP codes, VK error codes, Yandex `ErrSessionExpired`, GBP API status strings) | All 4 agents |
| `NewDedupeClient(redisURL string)` | Dial Redis + return `*hitldedupe.DedupeClient` (returns `nil` on any failure; never blocks startup) | All 4 agents (`cmd/main.go`) |
| `GetEnv(key, defaultValue string)` | Env var lookup with fallback (empty == unset) | All 4 agents |

## Conventions

- Compile-time interface checks (`var _ TokenResolver = (*tokenResolverImpl)(nil)`) at file bottom.
- Constructors panic on `nil` REQUIRED deps (TokenResolver client); `nil` silently accepted for OPTIONAL deps (`Dispatcher`'s dedupe and classifier).
- No speculative methods — extracted from existing duplication only. A 5th platform with different needs should refactor the package, not extend it speculatively.
- Errors propagate verbatim from `tokenclient` and Redis — callers MUST NOT string-match.

## Tests

```bash
cd pkg && go test -race ./agentbase/...
```

Suite covers `tokenResolverImpl`, `dispatcherImpl` (dedupe-on / off / cached duplicate / in-flight / store-failure paths), `FuncClassifier` (nil + identity), and `NewDedupeClient` boot fallbacks.
