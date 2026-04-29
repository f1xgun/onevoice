'use client';

import { useState } from 'react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { Toaster } from 'sonner';

export function Providers({ children }: { children: React.ReactNode }) {
  const [queryClient] = useState(
    () =>
      new QueryClient({
        defaultOptions: {
          queries: {
            staleTime: 60_000,
            retry: 1,
          },
        },
      })
  );

  return (
    <QueryClientProvider client={queryClient}>
      {children}
      <Toaster
        position="top-right"
        // Linen motion: auto-dismiss after 5s unless the toast carries an
        // action (Sonner already keeps action toasts open until dismissed).
        // Slide-in timing is owned by Sonner internally — it ships a CSS
        // animation in the ~250ms range with prefers-reduced-motion handled
        // by the library; not configurable per-toast.
        duration={5000}
        toastOptions={{
          // Linen-toned toasts. richColors is too loud for the design;
          // we lean on tokens so the toast sits on top of paper instead
          // of saturated green/red surfaces.
          classNames: {
            toast:
              'border border-[var(--ov-line)] bg-[var(--ov-paper-raised)] text-[var(--ov-ink)] shadow-[var(--ov-shadow-2)]',
            description: 'text-[var(--ov-ink-mid)]',
            actionButton: 'bg-[var(--ov-ink)] text-[var(--ov-paper)]',
            cancelButton: 'bg-[var(--ov-paper-sunken)] text-[var(--ov-ink-mid)]',
            success: 'border-[oklch(0.85_0.06_145)]',
            error: 'border-[oklch(0.85_0.08_25)] text-[var(--ov-danger)]',
            warning: 'border-[oklch(0.85_0.10_75)] text-[var(--ov-warning-ink)]',
            info: 'border-[oklch(0.85_0.05_230)]',
          },
        }}
      />
    </QueryClientProvider>
  );
}
