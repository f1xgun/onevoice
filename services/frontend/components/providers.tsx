'use client';

import { QueryClientProvider } from '@tanstack/react-query';
import { Toaster } from 'sonner';
import { queryClient } from '@/lib/queryClient';

export function Providers({ children }: { children: React.ReactNode }) {
  return (
    <QueryClientProvider client={queryClient}>
      {children}
      <Toaster
        position="top-right"
        duration={5000}
        toastOptions={{
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
