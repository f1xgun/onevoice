'use client';

import { useMemo, useRef, useState } from 'react';
import { useForm } from 'react-hook-form';
import { zodResolver } from '@hookform/resolvers/zod';
import { useRouter } from 'next/navigation';
import Link from 'next/link';
import { useTranslations } from 'next-intl';
import { toast } from 'sonner';
import { api } from '@/lib/api';
import { API_PATHS } from '@/lib/constants/apiPaths';
import { useAuthStore } from '@/lib/auth';
import { queryClient } from '@/lib/queryClient';
import { BUSINESS_LIST_QUERY_KEY } from '@/lib/hooks/useBusinessList';
import { QUERY_KEYS } from '@/lib/constants/queryKeys';
import { createLoginSchema, type LoginInput } from '@/lib/schemas';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { AuthShell } from '@/components/auth/AuthShell';
import { MonoLabel } from '@/components/ui/mono-label';
import { SmartCaptcha, type SmartCaptchaHandle } from '@/components/auth/SmartCaptcha';

// HTTP 423 Locked — same status the LockoutMiddleware short-circuits with.
const HTTP_STATUS_LOCKED = 423;
// Defensive fallback when the backend omits retry_after_seconds.
const DEFAULT_LOCK_SECONDS = 900;
const SECONDS_PER_MINUTE = 60;

// Login error body shapes. account_locked carries retry_after_seconds;
// captcha_required / captcha_invalid carry only the code.
type LoginErrorBody = {
  code?: string;
  error?: string;
  retry_after_seconds?: number;
};

export default function LoginPage() {
  const router = useRouter();
  const setAuth = useAuthStore((s) => s.setAuth);
  const tLogin = useTranslations('auth.login');
  const tErrors = useTranslations('common.errors');
  const tValidation = useTranslations('validation');
  // Rebuild the schema on translator-identity change so locale-switch swaps
  // validation copy.
  const loginSchema = useMemo(() => createLoginSchema(tValidation), [tValidation]);

  // Captcha widget mounts lazily — first 400 captcha_required flips the flag
  // and the next submit fetches a token.
  const [captchaRequired, setCaptchaRequired] = useState(false);
  const captchaRef = useRef<SmartCaptchaHandle | null>(null);
  const [lockedRetrySeconds, setLockedRetrySeconds] = useState<number | null>(null);

  const captchaSiteKey = process.env.NEXT_PUBLIC_SMARTCAPTCHA_SITE_KEY ?? '';

  const {
    register,
    handleSubmit,
    formState: { errors, isSubmitting },
  } = useForm<LoginInput>({
    resolver: zodResolver(loginSchema),
  });

  const onSubmit = async (data: LoginInput) => {
    try {
      // If backend previously demanded a captcha, fetch a token now and
      // attach it as a header.
      const headers: Record<string, string> = {};
      if (captchaRequired && captchaRef.current && captchaSiteKey) {
        const token = await captchaRef.current.execute();
        headers['X-Captcha-Token'] = token;
      }
      const res = await api.post(API_PATHS.AUTH.LOGIN, data, { headers });
      setAuth(res.data.user, res.data.accessToken);
      // removeQueries (not invalidateQueries): invalidate keeps stale data
      // around, leaving a window where the new actor briefly observes the
      // previous user's perms. BUSINESS_LIST_QUERY_KEY is a prefix —
      // partial sweep also drops nested per-business keys. PERMISSIONS_CATALOG
      // is a separate top-level key, removed explicitly so a different-deploy
      // catalog can re-fetch.
      queryClient.removeQueries({ queryKey: BUSINESS_LIST_QUERY_KEY });
      queryClient.removeQueries({ queryKey: QUERY_KEYS.PERMISSIONS_CATALOG });
      setLockedRetrySeconds(null);
      setCaptchaRequired(false);
      router.push('/chat');
    } catch (err) {
      const response = (err as { response?: { status?: number; data?: LoginErrorBody } })?.response;
      const body = response?.data;
      const code = body?.code;

      // 423 account_locked → render the lockout panel. The middleware
      // short-circuits the request with this status before the handler
      // runs, so no other failure path can land here with 423.
      if (response?.status === HTTP_STATUS_LOCKED && code === 'account_locked') {
        setLockedRetrySeconds(body?.retry_after_seconds ?? DEFAULT_LOCK_SECONDS);
        return;
      }

      // 400 captcha_required → first time we hit this, mount the invisible
      // widget and prompt the user to retry. Subsequent submits will attach
      // the X-Captcha-Token header.
      if (code === 'captcha_required') {
        setCaptchaRequired(true);
        toast.error(tLogin('captchaRequired'));
        return;
      }

      // 403 captcha_invalid → keep the widget mounted so the next submit
      // triggers a fresh challenge.
      if (code === 'captcha_invalid') {
        toast.error(tLogin('captchaInvalid'));
        return;
      }

      // Any other code we know about → localize via the i18n catalog.
      if (code) {
        try {
          toast.error(tErrors(code));
          return;
        } catch {
          // Translation missing → fall through to the legacy fallback.
        }
      }

      const message = (err as { response?: { data?: { message?: string } } })?.response?.data
        ?.message;
      toast.error(message ?? tLogin('invalidCredentials'));
    }
  };

  // When locked, the form is replaced with a static panel explaining the
  // wait window and offering a password-reset CTA (clearing the password
  // resets the lockout counter — self-unlock).
  const lockedMinutes =
    lockedRetrySeconds !== null
      ? Math.max(1, Math.ceil(lockedRetrySeconds / SECONDS_PER_MINUTE))
      : 0;

  return (
    <AuthShell
      eyebrow={tLogin('eyebrow')}
      title={tLogin('title')}
      description={tLogin('description')}
      aside={<LoginEditorial />}
    >
      {lockedRetrySeconds !== null ? (
        <div
          role="alert"
          className="flex flex-col gap-4 rounded-md border border-[var(--ov-danger)] bg-paper-sunken p-6"
        >
          <h2 className="text-lg font-semibold text-ink">{tLogin('lockoutTitle')}</h2>
          <p className="text-sm leading-relaxed text-ink-mid">
            {tLogin('lockoutBody', { minutes: lockedMinutes })}
          </p>
          <Link
            href="/auth/password-reset"
            className="self-start text-sm font-medium text-ink underline hover:no-underline"
          >
            {tLogin('lockoutResetCta')}
          </Link>
        </div>
      ) : (
        <form onSubmit={handleSubmit(onSubmit)} className="flex flex-col gap-4">
          <div className="flex flex-col gap-1.5">
            <Label htmlFor="email" className="text-xs font-medium text-ink-mid">
              {tLogin('emailLabel')}
            </Label>
            <Input
              id="email"
              type="email"
              placeholder="vy@example.com"
              autoComplete="email"
              {...register('email')}
            />
            {errors.email && (
              <p className="text-sm text-[var(--ov-danger)]">{errors.email.message}</p>
            )}
          </div>

          <div className="flex flex-col gap-1.5">
            <Label htmlFor="password" className="text-xs font-medium text-ink-mid">
              {tLogin('passwordLabel')}
            </Label>
            <Input
              id="password"
              type="password"
              placeholder="••••••••"
              autoComplete="current-password"
              {...register('password')}
            />
            {errors.password && (
              <p className="text-sm text-[var(--ov-danger)]">{errors.password.message}</p>
            )}
          </div>

          {/* Invisible SmartCaptcha. Only mounted after the backend signals
              captcha_required AND a public site key is configured. The
              widget itself is aria-hidden — the visible "verify" prompt
              is the toast in onSubmit. */}
          {captchaRequired && captchaSiteKey && (
            <SmartCaptcha ref={captchaRef} siteKey={captchaSiteKey} />
          )}

          <Button type="submit" size="lg" className="mt-2 w-full" disabled={isSubmitting}>
            {isSubmitting ? tLogin('submitting') : tLogin('submit')}
          </Button>

          <p className="mt-6 text-sm text-ink-soft">
            {tLogin('noAccount')}{' '}
            <Link href="/register" className="font-medium text-ink hover:underline">
              {tLogin('register')}
            </Link>
          </p>
        </form>
      )}
    </AuthShell>
  );
}

function LoginEditorial() {
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
