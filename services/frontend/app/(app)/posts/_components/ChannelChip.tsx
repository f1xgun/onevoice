// app/(app)/posts/_components/ChannelChip.tsx — small platform chip
// rendered inside the posts-table "Платформы" column.
//
// Extracted from posts/page.tsx as part of.
import { useTranslations } from 'next-intl';
import { PlatformIcon } from '@/components/integrations/PlatformIcons';

import { PLATFORM_SHORT_KEYS } from '../_helpers';

export function ChannelChip({ platform }: { platform: string }) {
  const tShort = useTranslations('posts.platformShort');
  const shortLabel = PLATFORM_SHORT_KEYS.has(platform) ? tShort(platform) : platform;
  return (
    <span className="inline-flex items-center gap-1.5 rounded-full border border-line-soft bg-paper px-2 py-0.5 text-[11px] text-ink-mid">
      <PlatformIcon platform={platform} className="size-3.5" />
      {shortLabel}
    </span>
  );
}
