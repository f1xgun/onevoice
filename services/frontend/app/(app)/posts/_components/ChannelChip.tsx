// app/(app)/posts/_components/ChannelChip.tsx — small platform chip
// rendered inside the posts-table "Платформы" column.
//
// Extracted from posts/page.tsx as part of Phase 19 / 19-12.
import { ChannelMark } from '@/components/ui/channel-mark';
import { CHANNEL_NAMES } from '@/lib/platforms';

import { platformShort } from '../_helpers';

export function ChannelChip({ platform }: { platform: string }) {
  const display =
    CHANNEL_NAMES[platform as keyof typeof CHANNEL_NAMES] ?? platformShort[platform] ?? platform;
  const short = platformShort[platform] ?? display;
  return (
    <span className="inline-flex items-center gap-1.5 rounded-full border border-line-soft bg-paper px-2 py-0.5 text-[11px] text-ink-mid">
      <ChannelMark name={display} size={14} />
      {short}
    </span>
  );
}
