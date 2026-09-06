'use client';

import { useCallback, useEffect, useState } from 'react';
import Link from 'next/link';
import { useTranslations } from 'next-intl';
import { ArrowRight, Check, X } from 'lucide-react';
import { Button } from '@/components/ui/button';
import { MonoLabel } from '@/components/ui/mono-label';
import { useBusinessStore } from '@/lib/stores/business';
import { useOnboardingProgress, type OnboardingStep } from '@/hooks/useOnboardingProgress';
import { gettingStartedDismissKey, readDismissed, writeDismissed } from '@/lib/onboarding/dismiss';
import { cn } from '@/lib/utils';

export interface GettingStartedChecklistProps {
  /**
   * Opens the first-action wizard for the "Run your first AI action" step.
   * When omitted, that step deep-links to /chat.
   */
  onOpenWizard?: () => void;
  /**
   * When false (dedicated /getting-started page) the card ignores the
   * localStorage dismiss, omits the close button, and shows a "setup complete"
   * state once every gating step is done instead of unmounting. Default true.
   */
  dismissible?: boolean;
  className?: string;
}

export function GettingStartedChecklist({
  onOpenWizard,
  dismissible = true,
  className,
}: GettingStartedChecklistProps) {
  const activeBusinessId = useBusinessStore((s) => s.activeBusinessId);
  const dismissKey = gettingStartedDismissKey(activeBusinessId);
  const [dismissed, setDismissed] = useState<boolean>(() =>
    dismissible ? readDismissed(dismissKey) : false
  );

  // Re-read on business switch — the dismiss key is per-organization, and the
  // useState initializer only runs once.
  useEffect(() => {
    if (!dismissible) return;
    setDismissed(readDismissed(dismissKey));
  }, [dismissKey, dismissible]);

  const handleDismiss = useCallback(() => {
    writeDismissed(dismissKey);
    setDismissed(true);
  }, [dismissKey]);

  if (dismissible && dismissed) return null;

  return (
    <ChecklistBody
      onOpenWizard={onOpenWizard}
      dismissible={dismissible}
      onDismiss={handleDismiss}
      className={className}
    />
  );
}

interface ChecklistBodyProps {
  onOpenWizard?: () => void;
  dismissible: boolean;
  onDismiss: () => void;
  className?: string;
}

// Split out so the query-heavy useOnboardingProgress hook never mounts once the
// card is dismissed (the parent returns null before rendering this).
function ChecklistBody({ onOpenWizard, dismissible, onDismiss, className }: ChecklistBodyProps) {
  const t = useTranslations('gettingStarted');
  const progress = useOnboardingProgress();

  // Auto-hide once complete: persist the dismiss so it stays hidden across
  // reloads. Only in dismissible mode — the /getting-started page keeps
  // showing a completed state.
  useEffect(() => {
    if (dismissible && progress.allDone) onDismiss();
  }, [dismissible, progress.allDone, onDismiss]);

  if (dismissible && progress.allDone) return null;

  const complete = !dismissible && progress.allDone;
  const pct = progress.total > 0 ? Math.round((progress.completedCount / progress.total) * 100) : 0;

  return (
    <section
      aria-label={t('title')}
      className={cn(
        'rounded-lg border border-line bg-paper-raised p-5 text-left shadow-sm',
        className
      )}
    >
      <div className="flex items-start justify-between gap-3">
        <div className="min-w-0">
          <MonoLabel tone="ochre">{t('caption')}</MonoLabel>
          <h2 className="mt-1 text-lg font-medium tracking-tight text-ink">{t('title')}</h2>
          <p className="mt-1 text-[13px] text-ink-mid">
            {complete ? t('complete.body') : t('sub')}
          </p>
        </div>
        {dismissible && (
          <Button
            variant="ghost"
            size="sm"
            onClick={onDismiss}
            aria-label={t('dismissAria')}
            className="shrink-0"
          >
            <X className="h-4 w-4" />
          </Button>
        )}
      </div>

      <div className="mt-4">
        <div className="flex items-center justify-between text-[13px] text-ink-mid">
          <span>
            {complete
              ? t('complete.title')
              : t('progress', { done: progress.completedCount, total: progress.total })}
          </span>
          <span className="font-mono text-[11px] text-ink-soft">{pct}%</span>
        </div>
        <div
          role="progressbar"
          aria-valuenow={progress.completedCount}
          aria-valuemin={0}
          aria-valuemax={progress.total}
          aria-label={t('progressAria')}
          className="mt-1.5 h-1.5 w-full overflow-hidden rounded-full bg-paper-sunken"
        >
          <div
            className="h-full rounded-full bg-ochre transition-[width] duration-300 ease-out motion-reduce:transition-none"
            style={{ width: `${pct}%` }}
          />
        </div>
      </div>

      <ol className="mt-4 flex flex-col gap-1">
        {progress.steps
          .filter((step) => step.gating)
          .map((step) => (
            <StepRow key={step.id} step={step} onOpenWizard={onOpenWizard} />
          ))}
      </ol>
      {progress.steps.some((step) => !step.gating) && (
        <div className="mt-4 border-t border-line pt-4">
          <p className="text-sm text-ink-soft">{t('optional')}</p>
          <ul>
            {progress.steps
              .filter((step) => !step.gating)
              .map((step) => (
                <StepRow key={step.id} step={step} />
              ))}
          </ul>
        </div>
      )}
    </section>
  );
}

function StepRow({ step, onOpenWizard }: { step: OnboardingStep; onOpenWizard?: () => void }) {
  const t = useTranslations('gettingStarted');
  const label = t(`steps.${step.id}.label`);
  const hint = t(`steps.${step.id}.hint`);
  const showWizardHandler = step.id === 'firstAction' && !!onOpenWizard;

  return (
    <li
      data-testid={`onboarding-step-${step.id}`}
      data-done={step.done ? 'true' : 'false'}
      className="flex items-center gap-3 rounded-md px-2 py-2 transition-colors hover:bg-paper-sunken"
    >
      <StepMarker done={step.done} loading={step.loading} />
      <div className="min-w-0 flex-1">
        <p
          className={cn(
            'text-sm font-medium',
            step.done ? 'text-ink-soft line-through' : 'text-ink'
          )}
        >
          {label}
        </p>
        {!step.done && <p className="text-[13px] text-ink-mid">{hint}</p>}
      </div>
      {!step.done &&
        (showWizardHandler ? (
          <Button variant="secondary" size="sm" onClick={onOpenWizard} className="shrink-0">
            {t(`steps.${step.id}.cta`)}
            <ArrowRight className="h-3.5 w-3.5" />
          </Button>
        ) : (
          <Button asChild variant="secondary" size="sm" className="shrink-0">
            <Link href={step.href}>
              {t(`steps.${step.id}.cta`)}
              <ArrowRight className="h-3.5 w-3.5" />
            </Link>
          </Button>
        ))}
    </li>
  );
}

function StepMarker({ done, loading }: { done: boolean; loading: boolean }) {
  if (done) {
    return (
      <span
        aria-hidden
        className="flex h-6 w-6 shrink-0 items-center justify-center rounded-full bg-[var(--ov-success-soft)] text-[var(--ov-success)]"
      >
        <Check className="h-3.5 w-3.5" />
      </span>
    );
  }
  return (
    <span
      aria-hidden
      className={cn(
        'flex h-6 w-6 shrink-0 items-center justify-center rounded-full border border-line',
        loading ? 'animate-pulse bg-paper-sunken' : 'bg-paper'
      )}
    >
      <span className="h-1.5 w-1.5 rounded-full bg-ink-faint" />
    </span>
  );
}
