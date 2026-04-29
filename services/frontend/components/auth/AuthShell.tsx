// components/auth/AuthShell.tsx — Two-pane Linen auth layout.
// Form on the left, editorial visual on the right. Per
// design_handoff_onevoice 2/mocks/mock-auth.jsx (Login + Register).
//
// On md+ the grid is 1fr 1fr; on smaller viewports the editorial
// pane collapses and only the form column shows. Both panes get the
// same calm paper background, only the right one is paper-sunken.

import * as React from 'react';
import Link from 'next/link';
import { MonoLabel } from '@/components/ui/mono-label';

export interface AuthShellProps {
  /** mono caption above the headline ("Вход", "Создание аккаунта", …). */
  eyebrow: React.ReactNode;
  /** Big graphite headline ("С возвращением.", "Расскажите о бизнесе.", …). */
  title: React.ReactNode;
  /** Supporting copy below the headline, in ink-mid. */
  description: React.ReactNode;
  /** The form itself + its inline footer (register/login link, etc.). */
  children: React.ReactNode;
  /** Right-side editorial visual (quote, stat cards, preview). Hidden on mobile. */
  aside: React.ReactNode;
}

export function AuthShell({ eyebrow, title, description, children, aside }: AuthShellProps) {
  return (
    <div className="grid min-h-screen grid-cols-1 bg-background md:grid-cols-2">
      {/* Form column */}
      <div className="flex flex-col px-6 py-10 sm:px-12 md:px-16 md:py-12 lg:px-20">
        <Link
          href="/"
          className="inline-flex items-center gap-2.5 self-start"
          aria-label="OneVoice"
        >
          <span className="flex h-8 w-8 items-center justify-center rounded-md bg-ink text-base font-semibold tracking-tight text-paper">
            O
          </span>
          <span className="text-base font-semibold tracking-tight text-ink">OneVoice</span>
        </Link>

        <div className="my-auto flex w-full max-w-[420px] flex-col">
          <MonoLabel className="mb-2">{eyebrow}</MonoLabel>
          <h1 className="text-[40px] font-medium leading-[1.1] tracking-[-0.02em] text-ink">
            {title}
          </h1>
          <p className="mt-2 text-[15px] leading-[1.55] text-ink-mid">{description}</p>
          <div className="mt-8">{children}</div>
        </div>

        <MonoLabel className="mt-auto pt-8 text-ink-soft">
          © {new Date().getFullYear()} OneVoice ·{' '}
          <Link href="/terms" className="text-inherit hover:text-ink-mid">
            Условия
          </Link>{' '}
          ·{' '}
          <Link href="/privacy" className="text-inherit hover:text-ink-mid">
            Конфиденциальность
          </Link>
        </MonoLabel>
      </div>

      {/* Editorial column — hidden below md per spec. paper-sunken backdrop, line border on the inside edge. */}
      <aside className="hidden border-l border-line bg-paper-sunken px-12 py-12 md:flex md:flex-col md:justify-between lg:px-16">
        {aside}
      </aside>
    </div>
  );
}
