'use client';

// components/onboarding/GuidedCompose.tsx — the guided "compose a post" first
// action. It removes the blank-prompt barrier by letting the operator pick a
// post type and type a topic, then builds a single templated instruction and
// hands it to the EXISTING chat send path via `onCompose`. The chat loop
// drafts the post and the model's publish tool call surfaces the existing HITL
// approval card — this component writes no producer and no approval flow.

import { useCallback, useId, useState } from 'react';
import { useTranslations } from 'next-intl';
import { PenLine } from 'lucide-react';
import { Button } from '@/components/ui/button';
import { Label } from '@/components/ui/label';
import { Textarea } from '@/components/ui/textarea';
import { RadioGroup, RadioGroupItem } from '@/components/ui/radio-group';
import { trackEvent } from '@/lib/telemetry';
import {
  COMPOSE_POST_TYPES,
  buildComposeInstruction,
  isComposePostType,
  type ComposePostType,
} from '@/lib/compose-instruction';

export interface GuidedComposeProps {
  /** Seeds the composed instruction into the existing chat loop (sendMessage). */
  onCompose: (instruction: string) => void;
  /** Mirrors the composer-disabled contract so a compose can't race a stream. */
  disabled?: boolean;
  className?: string;
}

export function GuidedCompose({ onCompose, disabled = false, className }: GuidedComposeProps) {
  const t = useTranslations('gettingStarted.compose');
  const [open, setOpen] = useState(false);
  const [postType, setPostType] = useState<ComposePostType>('announcement');
  const [topic, setTopic] = useState('');
  const topicFieldId = useId();

  const instruction = buildComposeInstruction(postType, topic);
  const canSubmit = instruction !== null && !disabled;

  const submit = useCallback(() => {
    const composed = buildComposeInstruction(postType, topic);
    if (composed === null || disabled) return;
    trackEvent('activation', 'guided_compose', { metadata: { postType } });
    onCompose(composed);
    setTopic('');
    setOpen(false);
  }, [postType, topic, disabled, onCompose]);

  if (!open) {
    return (
      <button
        type="button"
        onClick={() => setOpen(true)}
        disabled={disabled}
        className={
          'inline-flex items-center gap-2 rounded-full border border-line bg-paper-raised px-4 py-2 text-sm text-ink-mid transition-colors hover:bg-paper-sunken hover:text-ink disabled:cursor-not-allowed disabled:opacity-50 ' +
          (className ?? '')
        }
      >
        <PenLine size={14} aria-hidden />
        {t('trigger')}
      </button>
    );
  }

  return (
    <section
      aria-label={t('title')}
      className={
        'w-full max-w-md space-y-4 rounded-md border border-line bg-paper-raised p-4 text-left ' +
        (className ?? '')
      }
    >
      <div className="space-y-1">
        <p className="text-sm font-medium text-ink">{t('title')}</p>
        <p className="text-[13px] text-ink-mid">{t('subtitle')}</p>
      </div>

      <fieldset className="space-y-2">
        <legend className="mb-1 text-xs font-medium text-ink-soft">{t('typeLabel')}</legend>
        <RadioGroup
          value={postType}
          onValueChange={(next) => {
            if (isComposePostType(next)) setPostType(next);
          }}
        >
          {COMPOSE_POST_TYPES.map((type) => (
            <label
              key={type}
              className="flex cursor-pointer items-center gap-2 text-sm text-ink-mid"
            >
              <RadioGroupItem value={type} />
              {t(`types.${type}`)}
            </label>
          ))}
        </RadioGroup>
      </fieldset>

      <div className="space-y-1.5">
        <Label htmlFor={topicFieldId} className="text-xs font-medium text-ink-soft">
          {t('topicLabel')}
        </Label>
        <Textarea
          id={topicFieldId}
          value={topic}
          onChange={(e) => setTopic(e.target.value)}
          placeholder={t('topicPlaceholder')}
          rows={3}
          className="border-line bg-paper text-ink"
        />
      </div>

      <div className="flex justify-end gap-2">
        <Button variant="ghost" size="sm" onClick={() => setOpen(false)}>
          {t('cancel')}
        </Button>
        <Button variant="primary" size="sm" onClick={submit} disabled={!canSubmit}>
          {t('submit')}
        </Button>
      </div>
    </section>
  );
}
