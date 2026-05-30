// Shared shell for /legal/{privacy,terms,consent,contact}.
// Server Component (no 'use client'): wraps the page body in an article
// container with a 70ch reading width per UI-SPEC §A Spacing. Footer is
// mounted by the parent (public)/layout.tsx.

import type { ReactNode } from 'react';

export default function LegalLayout({ children }: { children: ReactNode }) {
  return (
    <div className="mx-auto max-w-[70ch] px-4 py-8 md:px-6 md:py-12">
      <article className="text-[var(--ov-ink)]">{children}</article>
    </div>
  );
}
