# LLM Pricing — source of truth

Last verified: 2026-05-30

Sources:

- Anthropic: <https://platform.claude.com/docs/en/about-claude/pricing>
- OpenAI: <https://openai.com/api/pricing/>
- OpenRouter: zero per-token markup over upstream provider list price (<https://openrouter.ai/pricing>)

| Model ID                    | Input $/1M tok | Output $/1M tok |
| --------------------------- | -------------: | --------------: |
| anthropic/claude-sonnet-4-6 |           3.00 |           15.00 |
| anthropic/claude-haiku-4-5  |           1.00 |            5.00 |
| anthropic/claude-opus-4-7   |           5.00 |           25.00 |
| openai/gpt-4o-mini          |           0.15 |            0.60 |

Cache modifiers (Anthropic 5-minute ephemeral cache, per Phase 25a-02
`pkg/llm.Router.logBilling`):

- `CacheReadTokens` billed at 0.1× input rate
- `CacheCreationTokens` billed at 1.25× input rate
- `InputTokens` (post-cache) billed at 1.0× input rate

Commission (Phase 25a-02 `pkg/llm.CommissionConfig`):

- Mode: percentage
- Rate: 20% (informational only — v1.4 is free beta; the cost row carries
  `commission_usd` for reporting parity with the upstream provider cost but
  no end user is charged in v1.4).

## To update

1. Edit `services/orchestrator/internal/wire/llm.go` `modelPricing` map.
2. Edit this file's table AND bump "Last verified".
3. Extend `services/orchestrator/internal/wire/llm_test.go::TestPriceFor_KnownModel`
   to include the new model + assert its exact rates.
4. Run `make test-all` — the test enforces the dual-edit contract.

Forgetting any of steps 1–3 drops cost rows for that model to $0 (Pitfall §7)
and silently breaks Phase 25b's daily-spend rate limiter.

## Configured models in v1.4

Three orchestrator env vars resolve to model IDs in this table at boot:

| Env var             | Default                              | Typical override                 |
| ------------------- | ------------------------------------ | -------------------------------- |
| `LLM_MODEL`         | (required, no default)               | `anthropic/claude-sonnet-4-6`    |
| `DRAFT_REPLY_MODEL` | falls back to `LLM_MODEL` when unset | `anthropic/claude-haiku-4-5`     |
| `TITLER_MODEL`      | falls back to `LLM_MODEL` (api side) | `anthropic/claude-haiku-4-5`     |

The titler env var lives on the api service (see
`services/api/internal/config/config.go`); the other two on the orchestrator.
`wire.LLMRouter` registers a `ModelProviderEntry` for every configured model
× every provider whose API key is set, so cost-per-MTok flows uniformly
regardless of which provider Selector.Pick ends up routing the call through.
