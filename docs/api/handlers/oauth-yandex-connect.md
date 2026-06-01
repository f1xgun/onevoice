# OAuth Yandex.Business connect handler

`services/api/internal/handler/oauth/yandex_connect.go` implements the
paste-flow connect surface for Yandex.Business integrations. Yandex.Business
has no public OAuth REST API for reviews/info/posts (see
`MEMORY.md` → "Yandex.Business has no public OAuth API"); the agent talks
to Sprav via Playwright, and the modal's UX is "user pastes a cookie blob
exported from their browser". This handler is the API surface that
makes that paste-flow possible.

## Routes

```
POST /api/v1/integrations/yandex_business/probe
POST /api/v1/integrations/yandex_business/companies
POST /api/v1/integrations/yandex_business/connect
POST /api/v1/integrations/yandex_business/{id}/refresh-name
```

All four routes are auth + business-scoped (mounted under the
`/integrations` subtree). The shared dispatch helpers (`taskPublisher`,
`integrationService`, `httpClient`, `cfg`) come from the wider
`OAuthHandler` struct in `oauth/base.go`.

## Permissions

Every endpoint here requires `authz.PermIntegrationsConnect`. Probing
hits public Yandex endpoints, but the verdict CAN be used to confirm
the validity of someone else's session cookies, so the probe must NOT
be available to view-only members of the business. Connect and
list-companies obviously mutate state / spend agent capacity.

## Probe (`POST /probe`)

Validates pasted Yandex cookies WITHOUT persisting anything.

Always returns `200 OK` even on validation failure — the verdict is
encoded in the `ok` field of the response body. HTTP errors here would
be misread by the UI as network failures and the user would retry
unnecessarily.

Response shape (`yandexProbeResponse`):

| Field          | Semantics                                                                                                          |
|----------------|--------------------------------------------------------------------------------------------------------------------|
| `ok`           | Input parsed successfully. Does NOT imply session validity.                                                        |
| `format`       | Detected paste format (raw header, JSON dump, etc.) — drives the UI's per-format hint copy.                        |
| `session_valid`| Tri-state pointer. `true` → live HTTP probe confirmed login. `false` → probe redirected to login or returned 401/403. `nil` → probe failed (network/anti-bot) and we can't determine. |
| `username`     | Best-effort display name from the `yandex_login` cookie value. Empty when `session_valid != true`.                 |
| `warnings`     | Missing-but-recommended cookies (`sessionid2`, `yandex_login`).                                                    |
| `error`        | Localized error message when `ok=false`.                                                                           |

### Body-decode failure

`json.NewDecoder.Decode` failure → `200 OK` with
`{ok: false, error: i18n.Tr("oauth.yandex.invalid_body")}`. Not 400.
The probe modal is explicitly a "tell me what's wrong with my paste"
UX; a 4xx would short-circuit the explanation.

### Cookie parse

`yandexcookies.Parse` returns one of seven typed sentinel errors. The
`yandexCookiesErrorMessage` helper unwraps each into a localized
message via `pkg/i18n`:

| Sentinel                              | i18n key                                |
|---------------------------------------|------------------------------------------|
| `ErrEmpty`                            | `yandex.cookies.empty`                  |
| `ErrNoSessionID`                      | `yandex.cookies.missing_sessionid`      |
| `ErrInvalidJSON`                      | `yandex.cookies.invalid_format`         |
| `ErrSessionIDInvalid`                 | `yandex.cookies.invalid_sessionid`      |
| `ErrJSONUnmarshal` (wraps `json.SyntaxError`) | `yandex.cookies.json_error` + raw detail |
| anything else                         | `err.Error()` (untranslated fallback)   |

`errors.Is` matches wrapped errors — the `ErrJSONUnmarshal` arm strips
its own prefix from the wrapped string so only the underlying JSON
detail reaches the localized template.

### Live session probe

Best-effort. `probeYandexSession` builds a `GET` against
`h.yandexProbeURL()` (production: `business.yandex.ru`, overridable for
tests via `cfg.yandexProbeBaseURL`) with the pasted cookies in a
single `Cookie:` header. The client is a copy of `h.httpClient` with:

- `CheckRedirect` overridden to `http.ErrUseLastResponse` — we WANT to
  see the 302 to passport itself, not follow it into a login page.
- A 3s default timeout if the inherited client carried no timeout.
- A realistic Chrome User-Agent header to reduce the chance of being
  served a captcha-gate page.

Verdict:

- `200 OK` → session valid.
- `3xx` to `passport.yandex.*` host → not logged in.
- `3xx` elsewhere → assume live (rare; probably an internal redirect).
- `401`/`403` → not logged in.
- Anything else → return error so caller surfaces "unknown" verdict.

The username is sourced from the `yandex_login` cookie value when
present — no extra request, no HTML scraping, immune to anti-bot. The
Yandex SPA returns a JS shell rather than embedded user JSON, so HTML
parsing is unreliable; the cookie path is.

The probe context is `WithTimeout(r.Context(), 3*time.Second)` — wall-
clock cap of 3s regardless of what the inherited HTTP client carries.
Worst-case UX is "format OK, can't verify" (`session_valid: nil`),
which is already better than the legacy "paste and pray".

## List companies (`POST /companies`)

Dispatches the agent's `yandex_business__list_companies` RPA with the
caller's pasted cookies (NO integration row required) and returns the
list of orgs the user can pick from. Drives the connect modal's
company picker.

Synchronous: blocks the request for the duration of the Playwright run
(~25–45s). `yandexListCompaniesTimeout = 60 * time.Second` caps the
A2A RPC; `withRetry` inside the agent gives one retry.

503 if `taskPublisher` is nil (NATS dispatch unconfigured — dev
deploy without an agent fleet). 400 on body-decode / cookie-parse
failure. 502 on agent error (network failure or `resp.Error != ""`).
On success: `200 OK` with `{companies: [{permalink, name}, ...]}`.
The handler trims whitespace on both fields and drops any row whose
permalink is empty so the frontend never has to filter the list.

## Connect (`POST /connect`)

Persists pasted Yandex cookies as a new active integration. Mirrors
ConnectTelegram and the VK community connect.

Request body (`connectYandexRequest`):

| Field           | Required | Semantics                                                                              |
|-----------------|----------|----------------------------------------------------------------------------------------|
| `cookies`       | yes      | Paste blob. `yandexcookies.Parse` extracts the cookie set.                            |
| `permalink`     | no       | Sprav permalink — the modal's company-picker fills this when the picker ran.          |
| `business_name` | no       | Display name — the modal's company-picker fills this in.                              |

### External-ID resolution

The integration's `external_id` is the Sprav permalink. The handler
prefers the picker-supplied value so every subsequent agent call
(reviews, info, posts) builds correct `/sprav/<permalink>/p/edit` URLs
from the very first request:

```
externalID := "default"
if p := strings.TrimSpace(req.Permalink); p != "" {
    externalID = p
}
```

`"default"` is a placeholder for legacy `/connect` callers (or any
path that skipped the picker). `RefreshYandexBusinessName` heals
those rows asynchronously by replacing the placeholder with the real
Sprav permalink once the agent's `list_companies` RPA returns.

### Metadata

```
metadata := {
    "input_format": parsed.Format,
    "connected_at": time.Now().UTC().Format(time.RFC3339),
}
```

`business_name` is added when the picker supplied it; otherwise
`refresh-name` heals it later.

### Persistence

`integrationService.Connect` upserts on `(business_id, platform,
external_id)`. The pasted cookies are encrypted at the service layer
before reaching the database — the handler never sees the encrypted
form. On success: `201 Created` with the integration row.

## Refresh name (`POST /{id}/refresh-name`)

Backfills `metadata.business_name` and (when missing) the Sprav
permalink on an existing Yandex integration. The `/sprav/companies`
page is a SPA — server-rendered HTML is empty — so the handler
dispatches the agent's `yandex_business__list_companies` tool which
drives a real Playwright browser.

### Async fire-and-forget

The handler responds `202 Accepted` with
`{"status":"refresh_started"}` immediately and detaches the dispatch
into a goroutine:

```go
bgCtx, bgCancel := context.WithTimeout(context.Background(), yandexListCompaniesTimeout+15*time.Second)
go h.runYandexListCompaniesRefresh(bgCtx, bgCancel, integrationID, *target, bc.BusinessID)
```

The detached context is critical — Playwright RPA takes 20–40s, easily
longer than axios's idle timeout on the frontend, and the work must
complete even if the user navigates away mid-flight. `context.Background()`
guarantees the goroutine survives `r.Context()` cancellation; the
timeout caps absolute runtime (RPA + 15s slack for metadata writes).

The frontend polls `GET /integrations` to pick up the new
`business_name` once persisted.

### Healing legacy rows

`runYandexListCompaniesRefresh` always trusts the agent over whatever's
currently stored. The previous resolver (campaign-list/api) wrote
ad-campaign permalinks instead of Sprav permalinks; legacy rows
inherited `"default"` from the placeholder path; either way the
`list_companies`-derived permalink IS the canonical Sprav id, so we
overwrite whenever it differs:

```go
if permalink != "" && permalink != target.ExternalID {
    h.integrationService.UpdateExternalID(ctx, integrationID, permalink)
}
```

`business_name` updates merge into the existing metadata map rather
than replacing it, so any keys the connect path stamped
(`input_format`, `connected_at`) survive the refresh.

Lookup failures are non-fatal at every step: agent call failure,
empty company list, metadata write failure — all logged at INFO/ERROR
and the goroutine returns. The integration row is left in its
previous state. The frontend's next poll surfaces the unchanged row;
the user can retry the refresh.

## Cookie warnings

`cookieWarnings` flags missing-but-recommended cookies. `Session_id`
alone authenticates most Yandex.Business reads, but writes (reply
review, upload photo) need `sessionid2`, and Yandex's anti-CSRF flow
expects the `yandexuid` / `yandex_login` pair to be present. The
request is threaded into the helper so the warning copy can be
localized via `pkg/i18n`:

| Missing cookie    | i18n key                              |
|-------------------|----------------------------------------|
| `sessionid2`      | `oauth.yandex.missing_sessionid2`     |
| `yandex_login`    | `oauth.yandex.missing_yandex_login`   |

Warnings surface in the probe response only; the connect path persists
regardless because the warning set is advisory, not a validation gate.

## buildCookieHeader

Trivial helper: joins `name=value` pairs with `"; "`. Lives next to the
probe so a future change to the on-wire cookie format (e.g. URL-encoding
values that contain `;`) has a single call-site to update.
