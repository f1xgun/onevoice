'use client';

import { useMemo, useRef, useState } from 'react';
import { useForm } from 'react-hook-form';
import { zodResolver } from '@hookform/resolvers/zod';
import { useRouter } from 'next/navigation';
import Link from 'next/link';
import Image from 'next/image';
import { useTranslations } from 'next-intl';
import { api } from '@/lib/api';
import { API_PATHS } from '@/lib/constants/apiPaths';
import { useAuthStore } from '@/lib/auth';
import { resolvePostAuthRedirect } from '@/lib/postAuthRedirect';
import { queryClient } from '@/lib/queryClient';
import { BUSINESS_LIST_QUERY_KEY } from '@/lib/hooks/useBusinessList';
import { QUERY_KEYS } from '@/lib/constants/queryKeys';
import { createLoginSchema, type LoginInput } from '@/lib/schemas';
import { ActionButton as Button } from '@/components/design-system/ActionButton';
import { AppInput as Input } from '@/components/design-system/AppInput';
import { Label } from '@/components/ui/label';
import { AuthShell } from '@/components/auth/AuthShell';
import { SmartCaptcha, type SmartCaptchaHandle } from '@/components/auth/SmartCaptcha';

// HTTP 423 Locked — same status the LockoutMiddleware short-circuits with.
const HTTP_STATUS_LOCKED = 423;
const PRODUCT_PREVIEW = '/auth/product-preview.png';
const PRODUCT_PREVIEW_DARK = '/auth/product-preview-dark.png';
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
  const loginSchema = useMemo(() => createLoginSchema(tValidation), [tValidation]);

  const [captchaRequired, setCaptchaRequired] = useState(false);
  const captchaRef = useRef<SmartCaptchaHandle | null>(null);
  const [lockedRetrySeconds, setLockedRetrySeconds] = useState<number | null>(null);
  const [formError, setFormError] = useState<string | null>(null);

  const captchaSiteKey = process.env.NEXT_PUBLIC_SMARTCAPTCHA_SITE_KEY ?? '';

  const {
    register,
    handleSubmit,
    formState: { errors, isSubmitting },
  } = useForm<LoginInput>({
    resolver: zodResolver(loginSchema),
  });

  const onSubmit = async (data: LoginInput) => {
    setFormError(null);
    try {
      const headers: Record<string, string> = {};
      if (captchaRequired && captchaRef.current && captchaSiteKey) {
        const token = await captchaRef.current.execute();
        headers['X-Captcha-Token'] = token;
      }
      const res = await api.post(API_PATHS.AUTH.LOGIN, data, { headers });
      setAuth(res.data.user, res.data.accessToken);
      queryClient.removeQueries({ queryKey: BUSINESS_LIST_QUERY_KEY });
      queryClient.removeQueries({ queryKey: QUERY_KEYS.PERMISSIONS_CATALOG });
      setLockedRetrySeconds(null);
      setCaptchaRequired(false);
      router.push(resolvePostAuthRedirect(window.location.search));
    } catch (err) {
      const response = (err as { response?: { status?: number; data?: LoginErrorBody } })?.response;
      const body = response?.data;
      const code = body?.code;

      if (response?.status === HTTP_STATUS_LOCKED && code === 'account_locked') {
        setLockedRetrySeconds(body?.retry_after_seconds ?? DEFAULT_LOCK_SECONDS);
        return;
      }

      if (code === 'captcha_required') {
        setCaptchaRequired(true);
        setFormError(tLogin('captchaRequired'));
        return;
      }

      if (code === 'captcha_invalid') {
        setFormError(tLogin('captchaInvalid'));
        return;
      }

      if (code) {
        try {
          setFormError(tErrors(code));
          return;
        } catch {}
      }

      const message = (err as { response?: { data?: { message?: string } } })?.response?.data
        ?.message;
      setFormError(message ?? tLogin('invalidCredentials'));
    }
  };

  const lockedMinutes =
    lockedRetrySeconds !== null
      ? Math.max(1, Math.ceil(lockedRetrySeconds / SECONDS_PER_MINUTE))
      : 0;

  return (
    <AuthShell
      variant="access"
      eyebrow={null}
      title={tLogin('accessTitle')}
      description={null}
      aside={
        <figure>
          <div className="overflow-hidden rounded-inner border border-line">
            <Image
              src={PRODUCT_PREVIEW}
              alt={tLogin('previewAlt')}
              width={1280}
              height={900}
              priority
              className="h-auto w-full dark:hidden"
            />
            <Image
              src={PRODUCT_PREVIEW_DARK}
              alt={tLogin('previewAlt')}
              width={1280}
              height={900}
              priority
              className="hidden h-auto w-full dark:block"
            />
          </div>
          <figcaption className="mt-4 text-[15px] leading-[22px] text-ink-soft">
            {tLogin('previewCaption')}
          </figcaption>
        </figure>
      }
      footer={
        <>
          <p>
            {tLogin('betaPrompt')}
            <br />
            <Link href="/register">{tLogin('apply')}</Link>
          </p>
          <p>
            {tLogin('invitedPrompt')}
            <br />
            <Link href="/register?invited=1">{tLogin('createAccount')}</Link>
          </p>
        </>
      }
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
            <Label htmlFor="email" className="text-sm font-normal text-ink-soft">
              {tLogin('emailLabel')}
            </Label>
            <Input
              className="aria-[invalid=true]:border-danger aria-[invalid=true]:focus-visible:border-danger"
              id="email"
              aria-invalid={!!errors.email}
              aria-describedby={
                errors.email ? 'email-error' : formError ? 'login-error' : undefined
              }
              type="email"
              placeholder="vy@example.com"
              autoComplete="email"
              {...register('email')}
            />
            {errors.email && (
              <p id="email-error" role="alert" className="text-meta text-danger">
                {errors.email.message}
              </p>
            )}
          </div>

          <div className="flex flex-col gap-1.5">
            <Label htmlFor="password" className="text-sm font-normal text-ink-soft">
              {tLogin('passwordLabel')}
            </Label>
            <Input
              className="aria-[invalid=true]:border-danger aria-[invalid=true]:focus-visible:border-danger"
              id="password"
              aria-invalid={!!errors.password || !!formError}
              aria-describedby={
                errors.password ? 'password-error' : formError ? 'login-error' : undefined
              }
              type="password"
              placeholder="••••••••"
              autoComplete="current-password"
              {...register('password')}
            />
            {errors.password && (
              <p id="password-error" role="alert" className="text-meta text-danger">
                {errors.password.message}
              </p>
            )}
            {formError && (
              <p id="login-error" role="alert" className="text-sm leading-[22px] text-danger">
                {formError}
              </p>
            )}
            <Link
              href="/auth/password-reset"
              className="mt-2 self-end text-sm text-brand underline"
            >
              {tLogin('forgotPassword')}
            </Link>
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
        </form>
      )}
    </AuthShell>
  );
}
