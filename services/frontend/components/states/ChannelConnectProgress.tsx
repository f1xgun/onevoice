// components/states/ChannelConnectProgress.tsx — multi-step channel
// connect indicator with checkmarks.
//
// Mock anchor: design_handoff_onevoice 2/mocks/mock-states.jsx
// "Подключение канала: длительная операция" (lines 280–310). Used by
// the Telegram / VK connect modals while OAuth → bot-rights →
// message-sync stages run on the API.
//
// Step state vocabulary (controlled — caller drives transitions):
//   - 'done'    : checkmark, copy struck-through
//   - 'active'  : ochre ring, "в процессе" mono caption
//   - 'pending' : empty paper-sunken disc

import * as React from 'react';
import { Check } from 'lucide-react';
import { MonoLabel } from '@/components/ui/mono-label';
import { cn } from '@/lib/utils';

export type ChannelConnectStepState = 'pending' | 'active' | 'done';

export interface ChannelConnectStep {
  /** Stable id used as React key. */
  id: string;
  /** Step copy — short, factual. */
  label: string;
  state: ChannelConnectStepState;
}

export interface ChannelConnectProgressProps {
  /** Title at the top of the card — e.g. "Подключаемся к Telegram…". */
  title: React.ReactNode;
  steps: ChannelConnectStep[];
  className?: string;
}

export function ChannelConnectProgress({ title, steps, className }: ChannelConnectProgressProps) {
  return (
    <div
      role="status"
      aria-live="polite"
      aria-busy={steps.some((s) => s.state === 'active') ? 'true' : 'false'}
      className={cn(
        'flex max-w-[480px] flex-col gap-3 rounded-lg border border-line bg-paper-raised p-6',
        className
      )}
    >
      <div className="text-base font-medium text-ink">{title}</div>
      <ol className="m-0 flex list-none flex-col gap-2.5 p-0">
        {steps.map((step) => (
          <Step key={step.id} step={step} />
        ))}
      </ol>
    </div>
  );
}

function Step({ step }: { step: ChannelConnectStep }) {
  const { state, label } = step;
  return (
    <li className="flex items-center gap-3 text-sm">
      <span
        aria-hidden
        className={cn(
          'inline-flex size-[18px] shrink-0 items-center justify-center rounded-full',
          state === 'done' && 'border border-success bg-success text-paper',
          state === 'active' &&
            'border-[1.5px] border-ochre bg-ochre-soft text-[var(--ov-accent-ink)]',
          state === 'pending' && 'border border-line bg-paper-sunken'
        )}
      >
        {state === 'done' && <Check size={11} strokeWidth={3} />}
      </span>
      <span
        className={cn(
          state === 'done' && 'text-ink-soft line-through decoration-ink-faint',
          state === 'active' && 'text-ink',
          state === 'pending' && 'text-ink'
        )}
      >
        {label}
      </span>
      {state === 'active' && (
        <span className="ml-1">
          <MonoLabel>в процессе</MonoLabel>
        </span>
      )}
    </li>
  );
}
