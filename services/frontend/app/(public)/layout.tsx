import type { ReactNode } from 'react';

export default function PublicLayout({ children }: { children: ReactNode }) {
  // tabIndex={-1}: makes <main> programmatically focusable so the SkipLink
  // can actually transfer keyboard focus here (without it, hash navigation
  // only scrolls). focus:outline-none suppresses the focus ring that would
  // otherwise appear when focus lands here from the SkipLink.
  return (
    <main id="main-content" tabIndex={-1} className="focus:outline-none">
      {children}
    </main>
  );
}
