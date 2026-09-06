'use client';

// Surface 2: /auth/password-reset/confirm.
//
// Scanner-protection (PITFALLS §1.5): GET only renders this client-side
// page; the page mounts with the form HIDDEN behind a manual reveal
// button so headless prefetchers (Outlook Safe Links, Yandex 360) that
// follow one redirect but never interact cannot consume the token. The
// backend has no GET handler for this route — POST is the only consume
// path.
//
// Referrer-Policy: no-referrer is set by the sibling layout.tsx via
// Next.js metadata export — see that file for rationale.
//
// All copy comes from auth.passwordReset.confirm.* in messages/{ru,en}.json.

import { useMemo, useState } from 'react';
import { useRouter, useSearchParams } from 'next/navigation';
import Link from 'next/link';
import { useForm } from 'react-hook-form';
import { zodResolver } from '@hookform/resolvers/zod';
import { z } from 'zod';
import { useTranslations } from 'next-intl';

import { AuthShell } from '@/components/auth/AuthShell';
import { MonoLabel } from '@/components/ui/mono-label';
import { ActionButton as Button } from '@/components/design-system/ActionButton';
import { AppInput as Input } from '@/components/design-system/AppInput';
import { Label } from '@/components/ui/label';
import { mapErrorCode } from '@/lib/error_mapping';

// Backend POST /auth/password-reset/confirm reply contract:
// 204 → success (redirect to /login)
// 400 → {code: reset_token_invalid | reset_token_expired | password_too_weak}
// MIN_PASSWORD_LEN is the client-side validation floor (mirrors the
// backend handler validator `min=8` and PasswordResetService's len check).
const HTTP_NO_CONTENT = 204;
const MIN_PASSWORD_LEN = 8;

// PageState drives the reveal-then-form UX:
// - reveal       → only the «Задать новый пароль» CTA shown (form hidden)
// - form         → form mounted, user typing
// - submitting   → POST in flight, fields disabled
// - token_error  → 400 reset_token_invalid / reset_token_expired (or missing token)
// - weak_password → 400 password_too_weak — form stays mounted so user retries
type PageState = 'reveal' | 'form' | 'submitting' | 'token_error' | 'weak_password';

export default function PasswordResetConfirmPage() {
  const t = useTranslations('auth.passwordReset.confirm');
  const tErrors = useTranslations(); // top-level for COPY i18nKey resolution
  const router = useRouter();
  const searchParams = useSearchParams();
  const token = searchParams.get('token') ?? '';

  const [state, setState] = useState<PageState>(token ? 'reveal' : 'token_error');
  const [errorCode, setErrorCode] = useState<string | undefined>(
    token ? undefined : 'reset_token_invalid'
  );

  const schema = useMemo(
    () =>
      z
        .object({
          newPassword: z.string().min(MIN_PASSWORD_LEN, t('newPasswordHelper')),
          confirmPassword: z.string().min(MIN_PASSWORD_LEN, t('newPasswordHelper')),
        })
        .refine((d) => d.newPassword === d.confirmPassword, {
          path: ['confirmPassword'],
          message: t('passwordsMismatch'),
        }),
    [t]
  );
  type FormValues = z.infer<typeof schema>;

  const {
    register,
    handleSubmit,
    watch,
    formState: { errors },
  } = useForm<FormValues>({
    resolver: zodResolver(schema),
  });

  const pw1 = watch('newPassword') ?? '';
  const pw2 = watch('confirmPassword') ?? '';
  const passwordsMatch = pw1.length >= MIN_PASSWORD_LEN && pw1 === pw2;

  const onSubmit = async (values: FormValues) => {
    setState('submitting');
    try {
      const res = await fetch('/api/v1/auth/password-reset/confirm', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ token, newPassword: values.newPassword }),
      });
      if (res.status === HTTP_NO_CONTENT) {
        router.push('/login?reset=success');
        return;
      }
      const body = (await res.json().catch(() => ({}) as { code?: string })) as { code?: string };
      const code = body?.code;
      setErrorCode(code);
      setState(code === 'password_too_weak' ? 'weak_password' : 'token_error');
    } catch {
      setErrorCode(undefined);
      setState('token_error');
    }
  };

  const errorEntry = errorCode ? mapErrorCode(errorCode) : mapErrorCode('reset_token_invalid');

  return (
    <AuthShell
      eyebrow={t('eyebrow')}
      title={t('title')}
      description={t('description')}
      aside={<ConfirmPageEditorial />}
    >
      {state === 'reveal' && (
        <div className="flex flex-col gap-6">
          {/* Scanner-protection: form is intentionally hidden until the
              user clicks. A headless prefetcher that just follows the
              link will see only this button — no form to submit, no
              token consumed. */}
          <Button
            type="button"
            variant="accent"
            size="lg"
            className="w-full"
            onClick={() => setState('form')}
          >
            {t('revealCTA')}
          </Button>
        </div>
      )}

      {state === 'token_error' && (
        <div role="alert" className="flex flex-col gap-4">
          <p className="text-[15px] leading-[1.55] text-[var(--ov-danger)]">
            {tErrors(errorEntry.i18nKey)}
          </p>
          <Button asChild variant="primary" size="lg">
            <Link href={errorEntry.actionHref ?? '/auth/password-reset'}>
              {errorEntry.actionLabel ? tErrors(errorEntry.actionLabel) : t('requestNewLinkCTA')}
            </Link>
          </Button>
        </div>
      )}

      {(state === 'form' || state === 'submitting' || state === 'weak_password') && (
        <form onSubmit={handleSubmit(onSubmit)} className="flex flex-col gap-4">
          <div className="flex flex-col gap-1.5">
            <Label htmlFor="newPassword" className="text-xs font-medium text-ink-mid">
              {t('newPasswordLabel')}
            </Label>
            <Input
              id="newPassword"
              type="password"
              autoComplete="new-password"
              disabled={state === 'submitting'}
              {...register('newPassword')}
            />
            <p className="text-xs text-ink-soft">{t('newPasswordHelper')}</p>
            {errors.newPassword && (
              <p className="text-sm text-[var(--ov-danger)]">{errors.newPassword.message}</p>
            )}
            {state === 'weak_password' && (
              <p role="alert" className="text-sm text-[var(--ov-danger)]">
                {tErrors(mapErrorCode('password_too_weak').i18nKey)}
              </p>
            )}
          </div>

          <div className="flex flex-col gap-1.5">
            <Label htmlFor="confirmPassword" className="text-xs font-medium text-ink-mid">
              {t('confirmPasswordLabel')}
            </Label>
            <Input
              id="confirmPassword"
              type="password"
              autoComplete="new-password"
              disabled={state === 'submitting'}
              {...register('confirmPassword')}
            />
            {pw2.length > 0 && !passwordsMatch && (
              <p className="text-sm text-[var(--ov-danger)]">{t('passwordsMismatch')}</p>
            )}
          </div>

          <Button
            type="submit"
            size="lg"
            className="mt-2 w-full"
            disabled={!passwordsMatch || state === 'submitting'}
          >
            {state === 'submitting' ? t('submitting') : t('submit')}
          </Button>
        </form>
      )}
    </AuthShell>
  );
}

function ConfirmPageEditorial() {
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
