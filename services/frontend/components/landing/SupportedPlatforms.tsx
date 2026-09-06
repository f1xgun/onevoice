'use client';

import { useTranslations } from 'next-intl';
import { CircleCheck, CircleHelp, Clock, CircleOff } from 'lucide-react';
import { usePlatforms } from '@/lib/hooks/usePlatforms';

const LANDING_PLATFORMS = [
  'telegram',
  'vk',
  'yandex_business',
  'google_business',
  '2gis',
  'avito',
  'whatsapp',
  'instagram',
  'ok',
] as const;

export function SupportedPlatforms() {
  const { data, isPending, isError } = usePlatforms();
  const tPlatforms = useTranslations('landing.platforms');
  const tStatus = useTranslations('landing.platformStatus');

  return (
    <ul className="mt-8 grid gap-x-8 sm:grid-cols-2 lg:grid-cols-3">
      {LANDING_PLATFORMS.map((id) => {
        const status = data?.find((platform) => platform.id === id)?.status;
        const editorial = id === 'instagram' || id === 'ok';
        const key = editorial
          ? 'unlisted'
          : isPending
            ? 'loading'
            : isError
              ? 'unknown'
              : status === 'active'
                ? 'have'
                : status === 'coming_soon'
                  ? 'soon'
                  : status === 'oauth_not_configured'
                    ? 'unconfigured'
                    : 'unknown';
        const Icon =
          key === 'have'
            ? CircleCheck
            : key === 'loading'
              ? Clock
              : key === 'unknown'
                ? CircleHelp
                : CircleOff;
        return (
          <li key={id} className="min-w-0 border-t border-line py-4">
            <h3 className="text-action">{tPlatforms(`${id}.display`)}</h3>
            <p className="mt-1 text-meta text-ink-soft">{tPlatforms(`${id}.meta`)}</p>
            <p className="mt-3 flex items-start gap-2 text-meta text-ink-soft">
              <Icon aria-hidden="true" className="h-5 w-5 shrink-0" />
              <span>{tStatus(key)}</span>
            </p>
          </li>
        );
      })}
    </ul>
  );
}
