// components/ui/mono-label.tsx — OneVoice (Linen) primitive
// Tiny uppercase JetBrains Mono caption — used for CHANNEL / STATE /
// STATUS captions, small data labels, and timestamp prefixes. Default
// color is ink-soft; bump via the `tone` prop for hierarchy.

import * as React from 'react';
import { cn } from '@/lib/utils';

type Tone = 'soft' | 'mid' | 'ink' | 'ochre';

const toneClass: Record<Tone, string> = {
  soft: 'text-ink-soft',
  mid: 'text-ink-mid',
  ink: 'text-ink',
  ochre: 'text-[var(--ov-accent-ink)]',
};

export interface MonoLabelProps extends React.HTMLAttributes<HTMLSpanElement> {
  tone?: Tone;
}

export function MonoLabel({ className, tone = 'soft', children, ...props }: MonoLabelProps) {
  return (
    <span
      className={cn(
        'font-mono text-[11px] font-medium uppercase tracking-[0.04em]',
        toneClass[tone],
        className
      )}
      {...props}
    >
      {children}
    </span>
  );
}
