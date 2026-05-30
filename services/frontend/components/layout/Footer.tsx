// Surface G: site-wide footer mounted in BOTH
// (public)/layout.tsx and (app)/layout.tsx. Surfaces the
// three legal links + operator copyright + PDN contact email so the
// user always has an in-product path to the policies and the data
// controller's inbox.

'use client';

import Link from 'next/link';
import { useTranslations } from 'next-intl';
import { loadLegalEntity } from '@/lib/legal/entity';

export function Footer() {
  const t = useTranslations('footer');
  const entity = loadLegalEntity();
  // SSR-stable: capture once per render; client and server agree because
  // server snapshots the year at request time (Next.js renders the (app)
  // layout per request). For SSR-static pages the year stays accurate
  // across rebuilds (next build runs at deploy time).
  const year = new Date().getFullYear();

  return (
    <footer
      role="contentinfo"
      className="mt-auto border-t border-[var(--ov-line)] px-4 py-6 text-[var(--ov-ink-mid)] md:flex md:items-center md:justify-between md:gap-8 md:px-6"
    >
      <nav
        aria-label={t('navAria')}
        className="mb-4 flex flex-col gap-2 text-[14px] leading-[1.4] md:mb-0 md:flex-row md:gap-6"
      >
        <Link
          href="/legal/privacy"
          className="hover:text-[var(--ov-ink)] hover:underline hover:underline-offset-2"
        >
          {t('links.privacy')}
        </Link>
        <Link
          href="/legal/terms"
          className="hover:text-[var(--ov-ink)] hover:underline hover:underline-offset-2"
        >
          {t('links.terms')}
        </Link>
        <Link
          href="/legal/consent"
          className="hover:text-[var(--ov-ink)] hover:underline hover:underline-offset-2"
        >
          {t('links.consent')}
        </Link>
      </nav>
      <div className="flex flex-col gap-1 text-[12px] leading-[1.4] md:items-end md:text-right">
        <span>{t('copyright', { year, entityName: entity.name })}</span>
        {entity.emailPdn && entity.emailPdn !== '—' ? (
          <a
            href={`mailto:${entity.emailPdn}`}
            aria-label={t('contactAria', { email: entity.emailPdn })}
            className="hover:text-[var(--ov-ink)] hover:underline hover:underline-offset-2"
          >
            {t('contact', { email: entity.emailPdn })}
          </a>
        ) : (
          <span>{t('contact', { email: '—' })}</span>
        )}
      </div>
    </footer>
  );
}
