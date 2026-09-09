import * as React from 'react';
import Link from 'next/link';
import { useTranslations } from 'next-intl';
import { AppearanceControls } from '@/components/design-system/AppearanceControls';
import { cn } from '@/lib/utils';

export interface AuthShellProps {
  /** Caption above the headline. */
  eyebrow: React.ReactNode;
  /** Page headline. */
  title: React.ReactNode;
  /** Supporting copy below the headline, in ink-mid. */
  description: React.ReactNode;
  /** The form itself + its inline footer (register/login link, etc.). */
  children: React.ReactNode;
  /** Right-side editorial visual (quote, stat cards, preview). Hidden on mobile. */
  aside?: React.ReactNode;
}

export function AuthShell({ eyebrow, title, description, children, aside }: AuthShellProps) {
  const tShell = useTranslations('auth.shell');
  return (
    <div
      data-ov-motion
      className={cn(
        'relative grid min-h-dvh min-w-0 scroll-py-24 grid-cols-1 bg-background',
        aside && 'md:grid-cols-2'
      )}
    >
      <div
        className={cn(
          'flex min-w-0 flex-col gap-8 px-4 py-6 sm:px-8',
          !aside && 'mx-auto w-full max-w-lg'
        )}
      >
        <div className="flex min-h-11 items-center justify-between gap-4">
          <Link
            href="/"
            className="inline-flex shrink-0 items-center gap-2.5"
            aria-label="OneVoice"
          >
            <span className="flex h-8 w-8 items-center justify-center rounded-md bg-ink text-base font-semibold tracking-tight text-paper">
              O
            </span>
            <span className="text-base font-semibold tracking-tight text-ink">
              {tShell('wordmark')}
            </span>
          </Link>
          <AppearanceControls />
        </div>

        <div className="my-auto flex w-full min-w-0 flex-col py-8">
          <p className="mb-2 text-meta text-ink-soft">{eyebrow}</p>
          <h1 className="text-page-title text-ink">{title}</h1>
          <p className="mt-2 text-reading text-ink-mid">{description}</p>
          <div className="mt-8">{children}</div>
        </div>

        <p className="mt-auto text-meta text-ink-soft">
          {tShell('footer', { year: new Date().getFullYear() })}
        </p>
      </div>
      {aside && (
        <aside
          aria-label={tShell('illustrationAria')}
          className="hidden border-l border-line bg-paper-sunken px-12 py-12 md:flex md:flex-col md:justify-between lg:px-16"
        >
          {aside}
        </aside>
      )}
    </div>
  );
}
