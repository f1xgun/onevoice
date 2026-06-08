// components/chat/TypingIndicator.tsx — the "OneVoice is working" affordance.
//
// Bouncing ink-faint discs beside a localized caption. UI-SPEC: operators need
// to *read* the state, not just see animated dots. Shared by MessageBubble in
// two spots: the empty streaming bubble (before any token arrives) and a quiet
// footer that stays visible for the rest of the streaming turn, so the signal
// never vanishes during the gap between tokens / tool iterations.

import { cn } from '@/lib/utils';

export function TypingIndicator({ label, className }: { label: string; className?: string }) {
  return (
    <span className={cn('flex items-center gap-2 text-ink-mid', className)} aria-label={label}>
      <span className="flex gap-1" aria-hidden="true">
        <span className="h-2 w-2 animate-bounce rounded-full bg-ink-faint [animation-delay:0ms]" />
        <span className="h-2 w-2 animate-bounce rounded-full bg-ink-faint [animation-delay:150ms]" />
        <span className="h-2 w-2 animate-bounce rounded-full bg-ink-faint [animation-delay:300ms]" />
      </span>
      <span className="text-xs">{label}</span>
    </span>
  );
}
