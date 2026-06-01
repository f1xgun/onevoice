# Platform registry

`pkg/domain/platform.go` exposes the canonical list of third-party platforms
OneVoice integrates with (or plans to integrate with). The registry drives:

- `GET /api/v1/platforms` — list shown on the integrations page.
- The frontend's connect/coming-soon/disabled card decision tree.
- The set of valid integration `platform` values accepted by the API.

Platform `ID`s match the `a2a.AgentID` constants for platforms that have an
agent (snake_case — `yandex_business`, `google_business`); coming-soon-only
entries follow the same casing so the frontend can join by `id` without
normalisation.

`Name` and `Description` are **not** serialized over the wire — the frontend
resolves both via its `messages/*.json` bundles
(`platforms.fullLabel.<id>`, `platforms.description.<id>`). They are kept on
the Go struct as empty values so in-process callers keep compiling, and
`json:"-"` ensures `/api/v1/platforms` only ships `id` + `status`. Telegram
clients (frontend, marketing landing) pin the on-disk schema in
`services/frontend/lib/api/platforms.ts`.

`Platforms()` returns the registry with all real platforms marked
`PlatformStatusActive`. Callers that know which credentials are present
(typically the API platforms handler) downgrade entries to
`PlatformStatusOAuthNotConfigured` before surfacing the list to clients.

## Status values

| Status | Meaning |
|---|---|
| `active` | Agent is implemented and OAuth credentials (or equivalent: bot token, service key) are present in the API config. |
| `coming_soon` | Declared in the registry but held back from MVP. May lack an agent (`2gis`, `avito`, `whatsapp`), or be implemented but intentionally hidden until product launch is approved (`google_business`). |
| `oauth_not_configured` | Agent exists but the deployment is missing required credentials. The frontend hides connect buttons to avoid leading the user into a broken flow. |

## Catalog

Listed in display order (the order `Platforms()` returns).

| ID | Display name (i18n key) | Default status | Auth model | Notes |
|---|---|---|---|---|
| `telegram` | `platforms.fullLabel.telegram` | `active` | Bot API token | MVP launch platform. |
| `vk` | `platforms.fullLabel.vk` | `active` | OAuth access token (paste flow) | MVP launch platform. |
| `yandex_business` | `platforms.fullLabel.yandex_business` | `active` | Cookie paste (Playwright RPA, no public OAuth API) | MVP launch platform. |
| `google_business` | `platforms.fullLabel.google_business` | `coming_soon` | Google Business Profile OAuth | Agent exists; held back from MVP until marketing + support are ready (promote to `active` later). |
| `2gis` | `platforms.fullLabel.2gis` | `coming_soon` | TBD | No agent yet. |
| `avito` | `platforms.fullLabel.avito` | `coming_soon` | TBD | No agent yet. |
| `whatsapp` | `platforms.fullLabel.whatsapp` | `coming_soon` | TBD | No agent yet. |

## Adding a new platform

1. Append a new `{ID: "...", Status: PlatformStatusComingSoon}` row to
   `Platforms()` in `pkg/domain/platform.go`.
2. Add `platforms.fullLabel.<id>` and `platforms.description.<id>` strings to
   every `services/frontend/messages/*.json` locale bundle.
3. (Optional) Pin the new ID in
   `services/frontend/lib/api/platforms.ts`.
4. When an agent ships and the platform launches, flip the status to
   `PlatformStatusActive`.
