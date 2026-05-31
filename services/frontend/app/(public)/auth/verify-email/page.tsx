'use client';

// Surface 3: /auth/verify-email?token=…
//
// Scanner-protection (PITFALLS §2.1): GET-side only renders this
// client-side page; the form is HIDDEN behind a manual «Подтвердить
// email» button so headless prefetchers (Outlook Safe Links, Yandex 360)
// that follow one redirect but never interact cannot consume the token.
// The backend has no GET handler for /auth/verify-email/confirm — POST
// is the only consume path.
//
// T-VE-02 mitigation: the confirm endpoint returns 204 with NO Set-Cookie.
// The user is not auto-logged-in by clicking the link.
//
// All copy comes from auth.verifyEmail.* in messages/{ru,en}.json.

import { useState } from 'react';
import { useRouter, useSearchParams } from 'next/navigation';
import { useTranslations } from 'next-intl';

import { AuthShell } from '@/components/auth/AuthShell';
import { Button } from '@/components/ui/button';
import { MonoLabel } from '@/components/ui/mono-label';

const HTTP_NO_CONTENT = 204;
// Pause between success-state render and dashboard redirect so the user
// has time to read the confirmation copy.
const POST_SUCCESS_REDIRECT_DELAY_MS = 1500;

type PageState = 'reveal' | 'submitting' | 'verified' | 'error';

export default function VerifyEmailPage() {
  const t = useTranslations('auth.verifyEmail');
  const tErrors = useTranslations('auth.verifyEmail.errors');
  const router = useRouter();
  const params = useSearchParams();
  const token = params.get('token') ?? '';

  const [state, setState] = useState<PageState>(token ? 'reveal' : 'error');
  const [errorCode, setErrorCode] = useState<string | null>(token ? null : 'verify_token_invalid');

  async function confirm() {
    setState('submitting');
    setErrorCode(null);
    try {
      const res = await fetch('/api/v1/auth/verify-email/confirm', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ token }),
      });
      if (res.status === HTTP_NO_CONTENT) {
        setState('verified');
        // Redirect after a short pause so the success copy is legible.
        setTimeout(() => router.push('/'), POST_SUCCESS_REDIRECT_DELAY_MS);
        return;
      }
      const body = (await res.json().catch(() => ({}))) as { code?: string };
      setErrorCode(body.code ?? 'verify_token_invalid');
      setState('error');
    } catch {
      setErrorCode('verify_token_invalid');
      setState('error');
    }
  }

  // Success state replaces the page per UI-SPEC Surface 3.
  if (state === 'verified') {
    return (
      <AuthShell
        eyebrow={t('eyebrow')}
        title={t('successTitle')}
        description={t('successBody')}
        aside={<VerifyEmailAside />}
      >
        <Button onClick={() => router.push('/')}>{t('toDashboard')}</Button>
      </AuthShell>
    );
  }

  return (
    <AuthShell
      eyebrow={t('eyebrow')}
      title={t('title')}
      description={t('description')}
      aside={<VerifyEmailAside />}
    >
      {state === 'error' ? (
        <div className="flex flex-col gap-3">
          <p className="text-[15px] leading-[1.55] text-danger" aria-live="polite">
            {tErrors(errorCode ?? 'verify_token_invalid')}
          </p>
          <Button variant="ghost" onClick={() => router.push('/')}>
            {t('toDashboard')}
          </Button>
        </div>
      ) : (
        <Button
          variant="accent"
          disabled={state === 'submitting' || !token}
          onClick={confirm}
          aria-live="polite"
        >
          {state === 'submitting' ? t('submitting') : t('confirmCta')}
        </Button>
      )}
    </AuthShell>
  );
}

// Editorial aside — matches the Linen auth-page pattern. Minimal copy;
// the form column carries the entire flow.
function VerifyEmailAside() {
  const t = useTranslations('auth.verifyEmail');
  return (
    <div className="flex h-full flex-col justify-between">
      <MonoLabel>{t('eyebrow')}</MonoLabel>
      <p className="text-[15px] leading-[1.55] text-ink-mid">{t('asideBody')}</p>
    </div>
  );
}
