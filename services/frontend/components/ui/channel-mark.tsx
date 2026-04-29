// components/ui/channel-mark.tsx — OneVoice (Linen) primitive
// Round platform mark — first letter on a paper-sunken disc, brand-tinted
// foreground. Used in chat messages, integration cards, channel chips.
//
// Default size 22px; bump via `size` prop. Per design_handoff_onevoice 2
// mock-ai-chat.jsx CMark (lines 9–31). Brand colors are intentionally
// muted variants of the canonical platform palette so they don't fight
// the warm paper background.

import * as React from 'react';
import { cn } from '@/lib/utils';

export type ChannelName =
  | 'Telegram'
  | 'VK'
  | 'Yandex.Business'
  | 'Yandex'
  | 'Google'
  | '2GIS'
  | 'OK'
  | 'WhatsApp'
  | 'Avito'
  | 'OneVoice'
  | 'You';

const channelColor: Record<string, string> = {
  Telegram: 'oklch(0.62 0.10 230)',
  VK: 'oklch(0.45 0.08 250)',
  'Yandex.Business': 'oklch(0.60 0.14 60)',
  Yandex: 'oklch(0.60 0.14 60)',
  Google: 'oklch(0.55 0.10 145)',
  '2GIS': 'oklch(0.62 0.13 145)',
  OK: 'oklch(0.65 0.13 60)',
  WhatsApp: 'oklch(0.55 0.13 145)',
  Avito: 'oklch(0.62 0.16 30)',
  OneVoice: 'var(--ov-accent)',
  You: 'var(--ov-ink)',
};

export interface ChannelMarkProps extends React.HTMLAttributes<HTMLSpanElement> {
  name: string;
  /** Pixel size — disc width and height. Default 22. */
  size?: number;
}

export function ChannelMark({ name, size = 22, className, style, ...props }: ChannelMarkProps) {
  const color = channelColor[name] ?? 'var(--ov-ink-mid)';
  const initial = (name || '?').charAt(0);
  return (
    <span
      aria-label={name}
      className={cn(
        'inline-flex shrink-0 items-center justify-center rounded-full border border-line bg-paper-sunken font-semibold',
        className
      )}
      style={{
        width: size,
        height: size,
        fontSize: Math.round(size * 0.42),
        color,
        ...style,
      }}
      {...props}
    >
      {initial}
    </span>
  );
}
