import type { ReactNode } from 'react';
import { Footer } from '@/components/layout/Footer';

export default function PublicLayout({ children }: { children: ReactNode }) {
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
