# «Своим голосом»: foundation verification

Date: 2026-09-06. Working branch: `codex/ds-1-tokens`.

## Baseline

- Working HEAD: `55dc19ca6926bd3becd45711d812130e957f85b0`.
- Stand checkout HEAD (read only): `5348015d27a10781e9a2aba14af4108331b916d0`.
- LIVE `http://localhost` returned 200. The frontend container started at
  `2026-09-05T21:55:34.094828548Z`; its image has no revision label.
  Checkout HEAD does **not** establish the running image revision.
- LIVE HTML contains `waitlist`, no `hero` ID and no `data-cta` attributes.
  Its public prices are Free `0 ₽`, Pro `3 990–4 990 ₽` per location per
  month, Enterprise `от 15 000 ₽`; beta pricing is fixed for one year.
- The working revision already has different entry markup and translations.
  Those pre-existing differences are not reverted to match LIVE.
- Working hybrid entry contract, verified in rendered ru/en HTML:
  `nav-login → /login`, `nav-register → /register`,
  `nav-waitlist → #waitlist`, `hero-waitlist → #waitlist`,
  `hero-login → /login`, `hero-register → /register`,
  `pricing-free-register → /register`, `pricing-pro-waitlist → #waitlist`.
  Both `hero` and `waitlist` IDs remain.
- Mobile bar conditions remain: hero is above viewport; waitlist is outside
  viewport; no editing field is focused; focused content is not covered by
  the 96px clearance. Mode-dependent destinations and tracking are unchanged.
- Translation bundles, `lib/landing-entry.ts`, `content/legal`, `lib/legal`,
  `pkg/legalconfig`, package.json and lockfile remain unchanged from working
  HEAD. Changes in legal React components are presentation imports only.
  Generated `components/ui/*` files remain unchanged.

Unchanged file SHA-256 values:

| File                   | SHA-256                                                            |
| ---------------------- | ------------------------------------------------------------------ |
| `lib/landing-entry.ts` | `fcd54e48dd26793df27f73d9fbf710bf587b7a9246822cd16ffd8c9d11ba095f` |
| `messages/ru.json`     | `829cfbd9d0276a26d50608c98201b26dbe5f7421048d45aac1c3a5248a04a284` |
| `messages/en.json`     | `473552384cb4cb79c39d0301974531d67dd7a8b0e690bd9d417b3d9385bc365f` |
| `pnpm-lock.yaml`       | `b826696dddf619b04674ab8608e7cade46c08b8df0b87fe5867f6941a912969a` |

## Evidence and limits

- Frozen installation uses Node 20.20.2, Next 15.3.9 and Tailwind **3.4.19**
  from the existing lockfile; package.json declares `^3.4.1`. No dependency
  changes were made.
- `styles.test.tsx` compiles real rendered component classes with Tailwind
  and checks the resulting declaration cascade and theme variable resolution.
  This caught the missing custom font-size groups in tailwind-merge.
  It covers primary/hover/disabled pairs, neutral/danger/link variants,
  control borders and focus rings. This is not a browser layout engine.
- Component tests exercise form submission, refs, slotted links, disabled
  events, advisory aria-disabled actions, dialog naming/Escape/return focus,
  calendar selection and explicit status announcements. Existing approval
  tests exercise decision and external-action contracts.
- Calendar buttons are supplied through `classNames` and `components`.
  Alert actions/cancels receive public `className` overrides. The generated
  ConfirmDestructive has no styling API, so its application composition uses
  exported alert primitives. No generated pagination component or chart
  consumer exists; custom pagination imports migrate with other buttons.
- The locally built application returned 200 for ru/en landing HTML.
  All ten font URLs in its CSS are application `/_next/static/media/*.woff2`
  URLs, fetched successfully with `Content-Type: font/woff2`.
  FontTools cmap inspection confirms Ёё, Йй, Щщ, ₽ and № in both families.
  Golos uses swap and Latin/Cyrillic preloads; Mono has preload disabled.
  CSS includes system fallbacks and no external font URLs.
- Ordinary `pnpm build` succeeds with APP_ENV unset. The existing production
  gate rejects `APP_ENV=production pnpm build` because these real controller
  fields are absent: `NEXT_PUBLIC_LEGAL_ENTITY_NAME`, `NEXT_PUBLIC_LEGAL_INN`,
  `NEXT_PUBLIC_LEGAL_ADDRESS`, `NEXT_PUBLIC_LEGAL_EMAIL_PDN`. No values were
  fabricated; production build acceptance remains conditional on real data.
- Both installed Chromium executables terminated with exit 134 in this
  restricted environment. Consequently the 375/1440 × light/dark × ru/en
  browser matrix for landing/history/login/approval, 320px reflow, enlarged
  text, custom spacing, cold browser font loading, blocked-font rendering,
  visual glyph inspection and screen-reader review remain **unverified**.
  Existing tests and HTTP/font checks do not replace that acceptance.
- The stand was only read. No containers, account data, external actions,
  legal settings or entry-mode configuration were changed.
