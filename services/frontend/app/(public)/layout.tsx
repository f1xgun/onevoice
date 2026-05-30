import type { ReactNode } from 'react';
import { Footer } from '@/components/layout/Footer';

export default function PublicLayout({ children }: { children: ReactNode }) {
  // Footer mounted in BOTH (public) and (app)
  // layouts so the three legal links + operator contact are reachable
  // from every page including /login, /register, /legal/*.
  //
  // tabIndex={-1}: makes <main> programmatically focusable so the SkipLink
  // can actually transfer keyboard focus here (without it, hash navigation
  // only scrolls). focus-visible:outline-ink (keyboard-only, not mouse) gives
  // a brief visible confirmation that focus actually moved here — required
  // by WCAG 2.4.7 since the skip-link's only purpose is to transfer focus.
  return (
    <div className="flex min-h-screen flex-col">
      <main
        id="main-content"
        tabIndex={-1}
        className="flex-1 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ink"
      >
        {children}
      </main>
      <Footer />
    </div>
  );
}
