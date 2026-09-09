import type { ReactNode } from 'react';

export default function Template({ children }: { children: ReactNode }) {
  return (
    <div data-ov-motion className="h-full min-h-0 motion-safe:animate-page-enter">
      {children}
    </div>
  );
}
