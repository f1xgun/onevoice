import Link from 'next/link';
import { ChevronRight } from 'lucide-react';
import { useTranslations } from 'next-intl';

const DESTINATIONS = ['rules', 'team', 'billing', 'account', 'data'] as const;

export default function SettingsPage() {
  const t = useTranslations('settings.v2');
  return (
    <section className="mx-auto max-w-[816px] px-gutter pb-10 pt-5 md:px-gutter-desktop">
      <h1 className="mb-title-gap text-page-title">{t('title')}</h1>
      <nav aria-label={t('title')} className="overflow-hidden rounded-block bg-block p-1 md:p-2">
        {DESTINATIONS.map((key) => (
          <Link
            key={key}
            href={`/settings/${key}`}
            className="flex min-h-control items-center justify-between gap-3 rounded-inner p-3 hover:bg-inset"
          >
            <span>
              <span className="block text-reading">{t(key)}</span>
              <span className="block text-meta text-ink-soft">{t(`${key}Hint`)}</span>
            </span>
            <ChevronRight aria-hidden className="h-head-sm w-head-sm shrink-0 text-ink-soft" />
          </Link>
        ))}
      </nav>
    </section>
  );
}
