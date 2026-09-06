'use client';

// Surface 1: /auth/password-reset (request page).
//
// Renders the email-only form inside <AuthShell>. On submit POSTs to
// /api/v1/auth/password-reset/request and ALWAYS shows the same
// confirmation panel afterwards regardless of whether the email was
// registered — the backend always responds 204 (no-enumeration contract
// + PITFALLS §1.1) so the frontend treats both branches
// identically.
//
// All copy comes from auth.passwordReset.request.* in messages/{ru,en}.json.

import { useMemo, useState } from 'react';
import Link from 'next/link';
import { useForm } from 'react-hook-form';
import { zodResolver } from '@hookform/resolvers/zod';
import { z } from 'zod';
import { useTranslations } from 'next-intl';

import { AuthShell } from '@/components/auth/AuthShell';
import { MonoLabel } from '@/components/ui/mono-label';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';

// Backend POST /auth/password-reset/request always replies 204 No Content
// on success — anything else is a network/proxy failure surface.
const HTTP_NO_CONTENT = 204;

type FormState = 'idle' | 'submitting' | 'sent' | 'error';

export default function PasswordResetRequestPage() {
  const t = useTranslations('auth.passwordReset.request');
  const [state, setState] = useState<FormState>('idle');

  const schema = useMemo(
    () =>
      z.object({
        email: z.string().trim().toLowerCase().email(t('emailInvalid')),
      }),
    [t]
  );
  type FormValues = z.infer<typeof schema>;

  const {
    register,
    handleSubmit,
    formState: { errors },
  } = useForm<FormValues>({
    resolver: zodResolver(schema),
  });

  const onSubmit = async (values: FormValues) => {
    setState('submitting');
    try {
      const res = await fetch('/api/v1/auth/password-reset/request', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ email: values.email }),
      });
      if (res.status !== HTTP_NO_CONTENT) {
        setState('error');
        return;
      }
      setState('sent');
    } catch {
      setState('error');
    }
  };

  return (
    <AuthShell
      eyebrow={t('eyebrow')}
      title={t('title')}
      description={t('description')}
      aside={<RequestPageEditorial />}
    >
      {state === 'sent' ? (
        <div role="status" aria-live="polite" className="flex flex-col gap-6">
          <p className="text-[15px] leading-[1.55] text-ink-mid">{t('sentBody')}</p>
          <Button asChild variant="ghost">
            <Link href="/login">{t('sentCTA')}</Link>
          </Button>
        </div>
      ) : (
        <form onSubmit={handleSubmit(onSubmit)} className="flex flex-col gap-4">
          <div className="flex flex-col gap-1.5">
            <Label htmlFor="email" className="text-xs font-medium text-ink-mid">
              {t('emailLabel')}
            </Label>
            <Input
              id="email"
              type="email"
              placeholder={t('emailPlaceholder')}
              autoComplete="email"
              disabled={state === 'submitting'}
              {...register('email')}
            />
            {errors.email && (
              <p className="text-sm text-[var(--ov-danger)]">{errors.email.message}</p>
            )}
          </div>

          {state === 'error' && (
            <p role="alert" className="text-sm text-[var(--ov-danger)]">
              {t('networkError')}
            </p>
          )}

          <Button type="submit" size="lg" className="mt-2 w-full" disabled={state === 'submitting'}>
            {state === 'submitting' ? t('submitting') : t('submit')}
          </Button>

          <p className="mt-6 text-sm text-ink-soft">
            <Link href="/login" className="font-medium text-ink hover:underline">
              {t('backToLogin')}
            </Link>
          </p>
        </form>
      )}
    </AuthShell>
  );
}

function RequestPageEditorial() {
  const t = useTranslations('auth.login.illustration');
  return (
    <>
      <MonoLabel>{t('label')}</MonoLabel>
      <p className="m-0 my-auto text-[34px] font-medium leading-[1.2] tracking-[-0.02em] text-ink">
        {t('body')}
      </p>
      <p className="text-sm leading-relaxed text-ink-soft">{t('note')}</p>
    </>
  );
}
