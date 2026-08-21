# LLM Pricing — source of truth

Last verified: 2026-08-21

Sources:

- Anthropic: <https://platform.claude.com/docs/en/about-claude/pricing>
- OpenAI: <https://openai.com/api/pricing/>
- OpenRouter: zero per-token markup over upstream provider list price (<https://openrouter.ai/pricing>)
- Yandex AI Studio (sync mode): <https://aistudio.yandex.ru/docs/ru/ai-studio/pricing.html>

| Model ID                    | Input $/1M tok | Output $/1M tok |
| --------------------------- | -------------: | --------------: |
| anthropic/claude-sonnet-4-6 |           3.00 |           15.00 |
| anthropic/claude-haiku-4-5  |           1.00 |            5.00 |
| anthropic/claude-opus-4-7   |           5.00 |           25.00 |
| openai/gpt-4o-mini          |           0.15 |            0.60 |
| deepseek-v4-flash           |           3.60 |            6.00 |

`deepseek-v4-flash` is priced natively in rubles by Yandex AI Studio: input
300 ₽/1M, output 500 ₽/1M incl. VAT. Converted here at CBR 83.36 ₽/$
(2026-08-21) → 3.60 / 6.00 $/1M; re-check the FX when refreshing. Registered
model IDs arrive folder-qualified (`gpt://<folder>/deepseek-v4-flash/latest`);
`priceFor` strips the scheme + folder + version to the bare slug before lookup.

Cache modifiers (applied by `pkg/llm.Router.logBilling`, provider-agnostic):

- `CacheReadTokens` billed at 0.1× input rate
- `CacheCreationTokens` billed at 1.25× input rate
- `InputTokens` (post-cache) billed at 1.0× input rate

Note: these ratios were tuned for the Anthropic 5-minute ephemeral cache. Yandex
AI Studio's implicit prefix cache prices cached tokens at 75 ₽ vs 300 ₽ input
(0.25× — a 4× discount), so the 0.1× read modifier under-bills a Yandex cache
hit relative to true cost. This is customer-favorable and conservative; revisit
if a per-provider cache ratio is introduced.

Commission (`pkg/llm.CommissionConfig`):

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

Forgetting any of steps 1–3 drops cost rows for that model to $0 and
silently breaks the daily-spend rate limiter.

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
