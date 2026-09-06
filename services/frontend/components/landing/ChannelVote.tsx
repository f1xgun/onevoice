'use client';

import { useState } from 'react';
import { useForm } from 'react-hook-form';
import { useTranslations } from 'next-intl';
import { Check } from 'lucide-react';

import { ActionButton as Button } from '@/components/design-system/ActionButton';
import { AppInput as Input } from '@/components/design-system/AppInput';
import { MonoLabel } from '@/components/ui/mono-label';
import { voteChannel } from '@/lib/api/waitlist';

// Direct-vote channels post immediately on click. "other" reveals a note field
// instead. Values are the backend PublicChannelVoteRequest wire tokens.
// Instagram is deliberately absent (Meta is blocked in RU) — latent demand is
// captured only through "other".
const DIRECT_CHANNELS = ['whatsapp', 'avito', '2gis'] as const;
const OTHER_CHANNEL = 'other';

interface OtherNoteForm {
  note: string;
}

export function ChannelVote() {
  const t = useTranslations('landing.channelVote');
  const [voted, setVoted] = useState<Set<string>>(new Set());
  const [otherOpen, setOtherOpen] = useState(false);
  const [failed, setFailed] = useState(false);

  const { register, handleSubmit, reset, formState } = useForm<OtherNoteForm>({
    defaultValues: { note: '' },
  });

  const markVoted = (channel: string) => {
    setVoted((prev) => {
      const next = new Set(prev);
      next.add(channel);
      return next;
    });
  };

  const castVote = async (channel: string, note?: string) => {
    setFailed(false);
    try {
      await voteChannel(note ? { channel, note } : { channel });
      markVoted(channel);
    } catch {
      setFailed(true);
    }
  };

  const onOtherSubmit = async (data: OtherNoteForm) => {
    await castVote(OTHER_CHANNEL, data.note.trim() || undefined);
    setOtherOpen(false);
    reset();
  };

  const anyVoted = voted.size > 0;

  return (
    <div className="mt-10 rounded-lg border border-line bg-paper-sunken p-6 sm:p-7">
      <MonoLabel tone="ochre">{t('kicker')}</MonoLabel>
      <h3 className="mt-2 text-[20px] font-medium leading-snug tracking-[-0.01em] sm:text-[22px]">
        {t('headline')}
      </h3>
      <p className="mt-2.5 max-w-[560px] text-[14px] leading-relaxed text-ink-mid">{t('body')}</p>

      <div className="mt-5 flex flex-wrap gap-2.5">
        {DIRECT_CHANNELS.map((channel) => {
          const isVoted = voted.has(channel);
          return (
            <Button
              key={channel}
              type="button"
              variant="secondary"
              size="md"
              disabled={isVoted}
              onClick={() => castVote(channel)}
            >
              {isVoted && <Check aria-hidden />}
              {t(`options.${channel}`)}
            </Button>
          );
        })}
        <Button
          type="button"
          variant="secondary"
          size="md"
          disabled={voted.has(OTHER_CHANNEL)}
          onClick={() => setOtherOpen((open) => !open)}
        >
          {voted.has(OTHER_CHANNEL) && <Check aria-hidden />}
          {t('options.other')}
        </Button>
      </div>

      {otherOpen && !voted.has(OTHER_CHANNEL) && (
        <form
          onSubmit={handleSubmit(onOtherSubmit)}
          className="mt-4 flex flex-col gap-2.5 sm:flex-row"
        >
          <Input
            placeholder={t('otherPlaceholder')}
            aria-label={t('otherPlaceholder')}
            maxLength={280}
            {...register('note')}
          />
          <Button type="submit" variant="primary" size="md" disabled={formState.isSubmitting}>
            {t('otherSubmit')}
          </Button>
        </form>
      )}

      {anyVoted && <p className="mt-4 text-[13px] text-ink-mid">{t('thanks')}</p>}
      {failed && <p className="mt-4 text-[13px] text-[var(--ov-danger)]">{t('error')}</p>}
    </div>
  );
}
