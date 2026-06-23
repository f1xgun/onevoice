# pkg/metrics — Label Cardinality Convention

All collectors in this package emit Prometheus metrics with bounded label
cardinality. Adding a new collector requires a doc-comment block citing the
rules below.

## Allowlist (safe labels)

| Label       | Allowed Values                                                                                                | Notes                                                                |
| ----------- | ------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------- |
| `model`     | known LLM model IDs                                                                                           | finite set per provider                                              |
| `provider`  | `openrouter`, `openai`, `anthropic`, `selfhosted`                                                             | closed set                                                           |
| `op`        | `find`, `insert`, `update`, `delete`, `aggregate`, `count`, `findAndModify`, `other`                          | mongo op whitelist; everything else collapses to `other`             |
| `result`    | `ok`, `error`, `timeout`                                                                                      | closed set                                                           |
| `status`    | `ok`, `error` plus per-collector tags                                                                         | finite                                                               |
| `subject`   | `tasks.telegram`, `tasks.vk`, `tasks.yandex_business`, `tasks.google_business`, `_INBOX`                      | `_INBOX.*` reply subjects collapse to `_INBOX` via CollapseSubject   |
| `job`       | scrape job name from prometheus.yml                                                                           | finite                                                               |
| `service`   | `api`, `orchestrator`, `agent-telegram`, `agent-vk`, `agent-yandex-business`, `agent-google-business`         | closed set                                                           |
| `tool_name` | `{platform}__{action}` tool-registry IDs                                                                      | finite (~20)                                                         |
| `agent_id`  | `telegram`, `vk`, `yandex_business`, `google_business`                                                        | closed set                                                           |
| `step`      | RPA step name hard-coded at the call site (`listCompanies`, `getInfo`, `getReviews`, `replyReview`, …)        | finite — never derive from runtime variables                         |
| `outcome`   | `chatturn.TurnOutcome.String()` set (`done`, `error`, `pause_hitl`, `reemitted_approval`, `rejoined_resume`, `orchestrator_unavailable`, `missing_message`, `business_not_found`, `inline_error`, `unknown`) | closed set — part of the chat-turn observability contract            |
| `decision`  | `approve`, `edit`, `reject`, `other`                                                                          | closed set — effective HITL verdict; `other` catches any unvalidated action string |

## Banlist (NEVER use as labels)

| Label                             | Why                                                                                |
| --------------------------------- | ---------------------------------------------------------------------------------- |
| `business_id`                     | per-tenant — explodes with user growth                                             |
| `user_id`                         | per-user — explodes with signups                                                   |
| `email`                           | per-user + PII                                                                     |
| `conversation_id`                 | per-conversation — millions/day                                                    |
| `request_id`                      | per-request — explodes immediately                                                 |
| Raw `_INBOX.<nuid>`               | per-request reply subject — always pass through `CollapseSubject`                  |
| `collection` (mongo)              | grows with schema; mongo_op label has `op` only                                    |
| `channel_id`                      | per-channel — explodes with integrations                                           |
| Free-form `screen` / `action`     | stay in Loki, do NOT promote to Prometheus labels                                  |

## Adding a new collector

1. Add a doc-comment block above the `var (...)` declaration listing the
   allowed values for each label.
2. Cite this README in the comment ("see pkg/metrics/README.md").
3. If a label needs a normalization step (like `CollapseSubject` for NATS
   reply subjects or `normalizeMongoOp` for mongo command names), implement
   it and call it at every observation site.
4. PR review checks the allowlist / banlist match.
