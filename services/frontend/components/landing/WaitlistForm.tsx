'use client';

import { useMemo, useState } from 'react';
import { Controller, useForm } from 'react-hook-form';
import { zodResolver } from '@hookform/resolvers/zod';
import { useTranslations } from 'next-intl';
import Link from 'next/link';
import { ArrowRight } from 'lucide-react';

import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { Checkbox } from '@/components/ui/checkbox';
import { RadioGroup, RadioGroupItem } from '@/components/ui/radio-group';
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select';
import { FormError } from '@/components/ui/form-error';
import { MonoLabel } from '@/components/ui/mono-label';
import { createWaitlistSchema, type WaitlistInput } from '@/lib/schemas';
import { joinWaitlist, type WaitlistPayload } from '@/lib/api/waitlist';
import { HTTP_STATUS } from '@/lib/constants/httpStatus';
import { TELEGRAM_CHANNEL_URL } from '@/lib/constants/landing';
import type { LandingEntryProps } from '@/lib/landing-entry';
import { legalDocHref } from '@/lib/legal/routes';

// Option values mirror the backend WaitlistRequest enums. Labels resolve from
// i18n; values are the wire tokens and must match the API allow-list.
const SPHERE_OPTIONS = ['cafe', 'beauty', 'services', 'retail', 'other'] as const;
const PAIN_OPTIONS = ['reviews', 'posts', 'card'] as const;

interface WaitlistFormProps extends Partial<LandingEntryProps> {
  source?: string;
  plan?: 'pro';
  submitLabel?: string;
}

export function WaitlistForm({ mode, source, plan, submitLabel }: WaitlistFormProps = {}) {
  const t = useTranslations('landing.waitlist');
  const tValidation = useTranslations('validation');
  const schema = useMemo(() => createWaitlistSchema(tValidation), [tValidation]);

  const {
    register,
    control,
    handleSubmit,
    formState: { errors, isSubmitting, isValid },
  } = useForm<WaitlistInput>({
    resolver: zodResolver(schema),
    mode: 'onChange',
    defaultValues: { email: '', sphere: undefined, pain: undefined },
  });

  const [submitted, setSubmitted] = useState(false);
  const [formError, setFormError] = useState<string | null>(null);

  const onSubmit = async (data: WaitlistInput) => {
    setFormError(null);
    const payload: WaitlistPayload = { email: data.email, consent: true };
    if (source) payload.source = source;
    if (plan) payload.plan = plan;
    if (data.sphere) payload.sphere = data.sphere;
    if (data.pain) payload.pain = data.pain;
    try {
      await joinWaitlist(payload);
      setSubmitted(true);
    } catch (err) {
      const status = (err as { response?: { status?: number } })?.response?.status;
      setFormError(status === HTTP_STATUS.TOO_MANY_REQUESTS ? t('errorRateLimit') : t('error'));
    }
  };

  if (submitted) {
    return (
      <div className="rounded-xl border border-line bg-paper-raised p-8 shadow-ov-3 sm:p-10">
        <MonoLabel tone="ochre">{t('success.label')}</MonoLabel>
        <h3 className="mt-3 text-[26px] font-medium leading-tight tracking-[-0.015em] sm:text-[30px]">
          {t('success.title')}
        </h3>
        <p className="mt-4 max-w-[440px] text-[15px] leading-relaxed text-ink-mid">
          {t('success.body')}
        </p>
        <div className="mt-7">
          <Button asChild size="lg" variant="primary">
            <a href={TELEGRAM_CHANNEL_URL} target="_blank" rel="noreferrer">
              {t('success.cta')}
              <ArrowRight aria-hidden />
            </a>
          </Button>
        </div>
        {(mode === 'hybrid' || mode === 'open') && (
          <Button asChild variant="secondary" className="mt-4 h-auto whitespace-normal text-center">
            <Link href="/register" data-cta="waitlist-success-register">
              {t('success.registerCta')}
            </Link>
          </Button>
        )}
      </div>
    );
  }

  return (
    <form
      onSubmit={handleSubmit(onSubmit)}
      noValidate
      className="rounded-xl border border-line bg-paper-raised p-6 shadow-ov-3 sm:p-8"
    >
      <div className="flex flex-col gap-5">
        <div className="flex flex-col gap-1.5">
          <Label htmlFor="waitlist-email" className="text-xs font-medium text-ink-mid">
            {t('emailLabel')}
          </Label>
          <Input
            id="waitlist-email"
            type="email"
            inputMode="email"
            autoComplete="email"
            placeholder={t('emailPlaceholder')}
            {...register('email')}
          />
          {errors.email && (
            <p className="text-sm text-[var(--ov-danger)]">{errors.email.message}</p>
          )}
        </div>

        <div className="flex flex-col gap-1.5">
          <Label htmlFor="waitlist-sphere" className="text-xs font-medium text-ink-mid">
            {t('sphereLabel')}
            <span className="ml-1.5 font-normal text-ink-soft">{t('optional')}</span>
          </Label>
          <Controller
            name="sphere"
            control={control}
            render={({ field }) => (
              <Select value={field.value} onValueChange={field.onChange}>
                <SelectTrigger id="waitlist-sphere">
                  <SelectValue placeholder={t('spherePlaceholder')} />
                </SelectTrigger>
                <SelectContent>
                  {SPHERE_OPTIONS.map((option) => (
                    <SelectItem key={option} value={option}>
                      {t(`sphereOptions.${option}`)}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            )}
          />
        </div>

        <fieldset className="flex flex-col gap-2.5">
          <legend className="mb-0.5 text-xs font-medium text-ink-mid">
            {t('painLabel')}
            <span className="ml-1.5 font-normal text-ink-soft">{t('optional')}</span>
          </legend>
          <Controller
            name="pain"
            control={control}
            render={({ field }) => (
              <RadioGroup value={field.value} onValueChange={field.onChange} className="gap-2.5">
                {PAIN_OPTIONS.map((option) => (
                  <label
                    key={option}
                    htmlFor={`pain-${option}`}
                    className="flex cursor-pointer items-center gap-2.5 text-[14px] text-ink"
                  >
                    <RadioGroupItem id={`pain-${option}`} value={option} />
                    {t(`painOptions.${option}`)}
                  </label>
                ))}
              </RadioGroup>
            )}
          />
        </fieldset>

        <div className="flex items-start gap-2.5">
          <Controller
            name="consent"
            control={control}
            render={({ field }) => (
              <Checkbox
                id="waitlist-consent"
                className="mt-0.5"
                checked={field.value === true}
                onCheckedChange={(value) => field.onChange(value === true)}
              />
            )}
          />
          <label htmlFor="waitlist-consent" className="text-[13px] leading-relaxed text-ink-mid">
            {t('consentLabel')}{' '}
            <Link href={legalDocHref('privacy')} className="text-ink underline hover:no-underline">
              {t('consentLinkText')}
            </Link>
          </label>
        </div>
        {errors.consent && (
          <p className="-mt-2 text-sm text-[var(--ov-danger)]">{errors.consent.message}</p>
        )}

        <FormError>{formError}</FormError>

        <Button
          type="submit"
          size="lg"
          variant="primary"
          className="w-full"
          disabled={isSubmitting || !isValid}
        >
          {isSubmitting ? t('submitting') : (submitLabel ?? t('submit'))}
        </Button>
      </div>
    </form>
  );
}
