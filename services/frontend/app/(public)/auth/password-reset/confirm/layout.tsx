// Server-side layout for /auth/password-reset/confirm. Emits the
// `Referrer-Policy: no-referrer` directive via Next.js metadata so the
// HTML <meta name="referrer" content="no-referrer" /> is present BEFORE
// any client JS runs — defends against the browser leaking the ?token=…
// query string in the Referer header of any subsequent navigation
// (PITFALLS §1.4 / D-16).
//
// This layout is server-side because the metadata export is only valid
// on server components; the child page itself stays a client component
// ('use client') because it needs useState / useForm / fetch.

import type { Metadata } from 'next';

export const metadata: Metadata = {
  referrer: 'no-referrer',
};

export default function PasswordResetConfirmLayout({ children }: { children: React.ReactNode }) {
  return <>{children}</>;
}
