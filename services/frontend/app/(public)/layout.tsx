import type { ReactNode } from 'react';

export default function PublicLayout({ children }: { children: ReactNode }) {
  // tabIndex={-1}: makes <main> programmatically focusable so the SkipLink
  // can actually transfer keyboard focus here (without it, hash navigation
  // only scrolls). focus-visible:outline-ink (keyboard-only, not mouse) gives
  // a brief visible confirmation that focus actually moved here — required
  // by WCAG 2.4.7 since the skip-link's only purpose is to transfer focus.
  return (
    <main
      id="main-content"
      tabIndex={-1}
      className="focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ink"
    >
      {children}
    </main>
  );
}
