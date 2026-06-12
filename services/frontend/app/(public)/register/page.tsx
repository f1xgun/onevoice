'use client';

import { useMemo, useState } from 'react';
import { useForm } from 'react-hook-form';
import { zodResolver } from '@hookform/resolvers/zod';
import { useRouter } from 'next/navigation';
import Link from 'next/link';
import { useTranslations } from 'next-intl';
import { api } from '@/lib/api';
import { API_PATHS } from '@/lib/constants/apiPaths';
import { HTTP_STATUS } from '@/lib/constants/httpStatus';
import { useAuthStore } from '@/lib/auth';
import { resolvePostAuthRedirect } from '@/lib/postAuthRedirect';
import { queryClient } from '@/lib/queryClient';
import { BUSINESS_LIST_QUERY_KEY } from '@/lib/hooks/useBusinessList';
import { QUERY_KEYS } from '@/lib/constants/queryKeys';
import { createRegisterSchema, type RegisterInput } from '@/lib/schemas';
import { TOS_VERSION, PRIVACY_VERSION, PDN_VERSION } from '@/lib/legal/versions';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { AuthShell } from '@/components/auth/AuthShell';
import { ConsentCheckboxes } from '@/components/auth/ConsentCheckboxes';
import { FormError } from '@/components/ui/form-error';
import { MonoLabel } from '@/components/ui/mono-label';

// zod schema field identifiers for the two
// required-consent checkboxes. Constants (not inline literals) so the
// i18next no-literal-string lint rule isn't false-tripped — these are
// schema keys, not user-facing copy.
const ACCEPT_TOS_PRIVACY_FIELD = 'acceptTosPrivacy' as const;
const ACCEPT_PDN_FIELD = 'acceptPdn' as const;

export default function RegisterPage() {
  const router = useRouter();
  const setAuth = useAuthStore((s) => s.setAuth);
  const tReg = useTranslations('auth.register');
  const tValidation = useTranslations('validation');
  const tRegErrors = useTranslations('register.errors');
  const registerSchema = useMemo(() => createRegisterSchema(tValidation), [tValidation]);

  const {
    register,
    handleSubmit,
    control,
    setError,
    formState: { errors, isSubmitting, isValid },
  } = useForm<RegisterInput>({
    resolver: zodResolver(registerSchema),
    mode: 'onChange',
  });
  const [formError, setFormError] = useState<string | null>(null);

  const onSubmit = async (data: RegisterInput) => {
    setFormError(null);
    try {
      const res = await api.post(API_PATHS.AUTH.REGISTER, {
        name: data.name,
        email: data.email,
        password: data.password,
        consents: {
          tos: TOS_VERSION,
          privacy: PRIVACY_VERSION,
          pdn: PDN_VERSION,
        },
      });
      setAuth(res.data.user, res.data.accessToken);
      queryClient.removeQueries({ queryKey: BUSINESS_LIST_QUERY_KEY });
      queryClient.removeQueries({ queryKey: QUERY_KEYS.PERMISSIONS_CATALOG });
      router.push(resolvePostAuthRedirect(window.location.search));
    } catch (err) {
      const response = (
        err as {
          response?: {
            status?: number;
            data?: { code?: string; message?: string; fields?: Record<string, string> };
          };
        }
      )?.response;
      const status = response?.status;
      const code = response?.data?.code;
      const message = response?.data?.message;
      const fields = response?.data?.fields;

      if (status === HTTP_STATUS.BAD_REQUEST && code === 'consent_required') {
        setFormError(tRegErrors('consentRequired'));
        return;
      }
      if (status === HTTP_STATUS.CONFLICT) {
        setError('email', { type: 'server', message: tReg('emailExists') });
        return;
      }
      // Surface backend field-level validation inline under the matching
      // input. Server field names are PascalCase (e.g. "Email"/"Password").
      if (fields) {
        let applied = false;
        for (const [rawKey, fieldMessage] of Object.entries(fields)) {
          const key = rawKey.toLowerCase();
          if ((key === 'email' || key === 'password' || key === 'name') && fieldMessage) {
            setError(key, { type: 'server', message: fieldMessage });
            applied = true;
          }
        }
        if (applied) return;
      }
      setFormError(message ?? tReg('genericError'));
    }
  };

  return (
    <AuthShell
      eyebrow={tReg('eyebrow')}
      title={tReg('title')}
      description={tReg('description')}
      aside={<RegisterEditorial />}
    >
      <form onSubmit={handleSubmit(onSubmit)} className="flex flex-col gap-4">
        <div className="flex flex-col gap-1.5">
          <Label htmlFor="name" className="text-xs font-medium text-ink-mid">
            {tReg('nameLabel')}
          </Label>
          <Input
            id="name"
            placeholder={tReg('namePlaceholder')}
            autoComplete="given-name"
            {...register('name')}
          />
          {errors.name && <p className="text-sm text-[var(--ov-danger)]">{errors.name.message}</p>}
        </div>

        <div className="flex flex-col gap-1.5">
          <Label htmlFor="email" className="text-xs font-medium text-ink-mid">
            {tReg('emailLabel')}
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

        <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
          <div className="flex flex-col gap-1.5">
            <Label htmlFor="password" className="text-xs font-medium text-ink-mid">
              {tReg('passwordLabel')}
            </Label>
            <Input
              id="password"
              type="password"
              placeholder="••••••••"
              autoComplete="new-password"
              {...register('password')}
            />
            {errors.password && (
              <p className="text-sm text-[var(--ov-danger)]">{errors.password.message}</p>
            )}
          </div>
          <div className="flex flex-col gap-1.5">
            <Label htmlFor="confirmPassword" className="text-xs font-medium text-ink-mid">
              {tReg('confirmPasswordLabel')}
            </Label>
            <Input
              id="confirmPassword"
              type="password"
              placeholder="••••••••"
              autoComplete="new-password"
              {...register('confirmPassword')}
            />
            {errors.confirmPassword && (
              <p className="text-sm text-[var(--ov-danger)]">{errors.confirmPassword.message}</p>
            )}
          </div>
        </div>

        {/* two required-consent checkboxes
            before the submit. Submit stays disabled until isValid (both
            literal(true)). */}
        <ConsentCheckboxes
          control={control}
          errors={errors}
          tosName={ACCEPT_TOS_PRIVACY_FIELD}
          pdnName={ACCEPT_PDN_FIELD}
        />

        <FormError>{formError}</FormError>

        <Button type="submit" size="lg" className="mt-2 w-full" disabled={isSubmitting || !isValid}>
          {isSubmitting ? tReg('submitting') : tReg('submit')}
        </Button>

        <p className="mt-6 text-sm text-ink-soft">
          {tReg('haveAccount')}{' '}
          <Link href="/login" className="font-medium text-ink hover:underline">
            {tReg('login')}
          </Link>
        </p>
      </form>
    </AuthShell>
  );
}

function RegisterEditorial() {
  const tReg = useTranslations('auth.register');
  const tBenefits = useTranslations('auth.register.benefits');
  const benefits = [
    { title: tBenefits('channelsTitle'), body: tBenefits('channelsBody') },
    { title: tBenefits('voiceTitle'), body: tBenefits('voiceBody') },
    { title: tBenefits('calmTitle'), body: tBenefits('calmBody') },
  ];
  return (
    <>
      <MonoLabel>{tReg('benefitsLabel')}</MonoLabel>

      <div className="my-auto flex flex-col gap-4">
        {benefits.map(({ title, body }) => (
          <div key={title} className="rounded-lg border border-line bg-paper-raised p-4">
            <div className="text-base font-medium leading-tight tracking-[-0.005em] text-ink">
              {title}
            </div>
            <p className="mt-1.5 text-sm leading-relaxed text-ink-mid">{body}</p>
          </div>
        ))}
      </div>

      <div className="rounded-md border border-line-soft bg-paper-raised p-4 text-sm leading-relaxed text-ink-mid">
        {tReg('benefitsBody')}
      </div>
    </>
  );
}
