import type { ReactNode } from 'react';

// PublicLayout — landmark wrapper for /login, /register, /onboarding,
// /invite/[token]. Before this layout, the public route group lacked a
// `<main>` landmark entirely, so axe-core's `region` rule reported every
// node in <body> as "content outside of landmarks" (5–9 nodes per page).
//
// The `id="main-content"` matches the SkipLink anchor mounted in the
// root `app/layout.tsx`. Keep both in lockstep — if you rename the
// anchor here, update `components/a11y/skip-link.tsx` too.
//
// This is intentionally a Server Component (no 'use client' directive)
// and renders no client-side state — it's a structural wrapper. Each
// nested page (login/register/onboarding/invite) keeps its own
// 'use client' directive where needed.
export default function PublicLayout({ children }: { children: ReactNode }) {
  return <main id="main-content">{children}</main>;
}
