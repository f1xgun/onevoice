// components/ui/approval-switch.tsx — OneVoice (Linen) primitive
// 4-mode segmented control for tool-approval defaults. Per
// design_handoff_onevoice 2/mocks/mock-settings.jsx ApprovalSwitch
// (lines 240–274).
//
// Modes (locked vocabulary, do not rename):
//   off             — Никогда
//   manual          — С вашего согласия
//   auto-with-review — Сам, но покажет
//   auto            — Полностью сам
//
// The recommended mode is marked with a small ochre disc at the
// top-right of its segment. The visual answers "what should I pick?"
// without dictating; the user keeps the final say.

'use client';

import * as React from 'react';
import { useTranslations } from 'next-intl';
import { cn } from '@/lib/utils';

export type ApprovalMode = 'off' | 'manual' | 'auto-with-review' | 'auto';

const ORDER: ApprovalMode[] = ['off', 'manual', 'auto-with-review', 'auto'];

// Hook returning locale-bound label record for the four approval modes.
export function useApprovalModeLabels(): Record<ApprovalMode, string> {
  const tModes = useTranslations('common.approvalSwitch.modes');
  return React.useMemo(
    () => ({
      off: tModes('off'),
      manual: tModes('manual'),
      'auto-with-review': tModes('auto-with-review'),
      auto: tModes('auto'),
    }),
    [tModes]
  );
}

export interface ApprovalSwitchProps {
  value: ApprovalMode;
  onValueChange: (value: ApprovalMode) => void;
  /** Optional recommended mode — marked with an ochre dot. */
  recommended?: ApprovalMode;
  disabled?: boolean;
  /** Accessible label for the radiogroup. Defaults to a localized fallback. */
  'aria-label'?: string;
  className?: string;
}

export function ApprovalSwitch({
  value,
  onValueChange,
  recommended,
  disabled,
  className,
  'aria-label': ariaLabel,
}: ApprovalSwitchProps) {
  const tSwitch = useTranslations('common.approvalSwitch');
  const labels = useApprovalModeLabels();
  const resolvedAriaLabel = ariaLabel ?? tSwitch('defaultAriaLabel');
  const refs = React.useRef<Array<HTMLButtonElement | null>>([]);

  function focusByIndex(idx: number) {
    const wrapped = (idx + ORDER.length) % ORDER.length;
    refs.current[wrapped]?.focus();
  }

  function onKeyDown(idx: number, e: React.KeyboardEvent<HTMLButtonElement>) {
    if (e.key === 'ArrowRight' || e.key === 'ArrowDown') {
      e.preventDefault();
      focusByIndex(idx + 1);
      onValueChange(ORDER[(idx + 1) % ORDER.length]);
    } else if (e.key === 'ArrowLeft' || e.key === 'ArrowUp') {
      e.preventDefault();
      focusByIndex(idx - 1);
      onValueChange(ORDER[(idx - 1 + ORDER.length) % ORDER.length]);
    } else if (e.key === 'Home') {
      e.preventDefault();
      focusByIndex(0);
      onValueChange(ORDER[0]);
    } else if (e.key === 'End') {
      e.preventDefault();
      focusByIndex(ORDER.length - 1);
      onValueChange(ORDER[ORDER.length - 1]);
    }
  }

  return (
    <div
      role="radiogroup"
      aria-label={resolvedAriaLabel}
      aria-disabled={disabled || undefined}
      className={cn(
        'inline-flex gap-0.5 rounded-md bg-paper-sunken p-0.5',
        disabled && 'opacity-50',
        className
      )}
    >
      {ORDER.map((mode, idx) => {
        const selected = value === mode;
        const isRecommended = recommended === mode;
        return (
          <button
            key={mode}
            ref={(el) => {
              refs.current[idx] = el;
            }}
            type="button"
            role="radio"
            aria-checked={selected}
            tabIndex={selected ? 0 : -1}
            disabled={disabled}
            onClick={() => onValueChange(mode)}
            onKeyDown={(e) => onKeyDown(idx, e)}
            className={cn(
              'duration-[120ms] relative rounded-sm px-3 py-1.5 text-xs font-medium transition-colors ease-out',
              'focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2',
              selected
                ? 'border border-line bg-paper text-ink shadow-ov-1'
                : 'border border-transparent text-ink-mid hover:text-ink',
              disabled && 'cursor-not-allowed'
            )}
          >
            {labels[mode]}
            {isRecommended && (
              <span
                aria-hidden
                title={tSwitch('recommendedTitle')}
                className="absolute -right-1 -top-1 h-2.5 w-2.5 rounded-full border-2 border-paper-raised bg-ochre"
              />
            )}
          </button>
        );
      })}
    </div>
  );
}
