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
  variant?: 'legacy' | 'access';
  footer?: React.ReactNode;
}

export function AuthShell({
  eyebrow,
  title,
  description,
  children,
  aside,
  variant = 'legacy',
  footer,
}: AuthShellProps) {
  const tShell = useTranslations('auth.shell');
  if (variant === 'access') {
    return (
      <div className="ov-app relative min-h-dvh bg-background px-gutter py-6 text-ink md:flex md:items-center md:py-12 md:pl-16 md:pr-12">
        <div
          className={cn(
            'mx-auto w-full max-w-[440px]',
            aside &&
              'md:grid md:max-w-none md:grid-cols-[392px_minmax(0,1fr)] md:items-center md:gap-12'
          )}
        >
          <div className="min-w-0">
            <Link
              href="/"
              aria-label="OneVoice"
              className="mb-5 inline-flex min-h-8 items-center gap-2.5 font-semibold"
            >
              <span className="flex h-8 w-8 items-center justify-center rounded-[8px] bg-ink text-sm text-paper">
                OV
              </span>
              {tShell('wordmark')}
            </Link>
            <div className="rounded-block bg-paper-raised p-4 md:p-5 [&_button[type=submit]]:min-h-12 [&_input]:min-h-12">
              {eyebrow && <p className="mb-3 text-sm text-ink-soft">{eyebrow}</p>}
              {title && <h1 className="text-2xl font-semibold leading-8">{title}</h1>}
              {description && (
                <p className="mt-3 text-[15px] leading-[22px] text-ink-soft">{description}</p>
              )}
              <div className={title || description ? 'mt-4' : undefined}>{children}</div>
            </div>
            {footer && (
              <div className="mt-5 space-y-3 text-[15px] leading-[22px] text-ink-soft [&_a]:text-brand [&_a]:underline">
                {footer}
              </div>
            )}
            <AppearanceControls className="mt-5 md:absolute md:bottom-6 md:left-16 md:mt-0" />
          </div>
          {aside && (
            <aside aria-label={tShell('illustrationAria')} className="hidden min-w-0 md:block">
              {aside}
            </aside>
          )}
        </div>
      </div>
    );
  }
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
