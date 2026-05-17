'use client';

import { useMemo } from 'react';
import { useForm } from 'react-hook-form';
import { zodResolver } from '@hookform/resolvers/zod';
import { useRouter } from 'next/navigation';
import Link from 'next/link';
import { useTranslations } from 'next-intl';
import { toast } from 'sonner';
import { api } from '@/lib/api';
import { API_PATHS } from '@/lib/constants/apiPaths';
import { HTTP_STATUS } from '@/lib/constants/httpStatus';
import { useAuthStore } from '@/lib/auth';
import { queryClient } from '@/lib/queryClient';
import { BUSINESS_LIST_QUERY_KEY } from '@/lib/hooks/useBusinessList';
import { QUERY_KEYS } from '@/lib/constants/queryKeys';
import { createRegisterSchema, type RegisterInput } from '@/lib/schemas';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { AuthShell } from '@/components/auth/AuthShell';
import { MonoLabel } from '@/components/ui/mono-label';

export default function RegisterPage() {
  const router = useRouter();
  const setAuth = useAuthStore((s) => s.setAuth);
  const tReg = useTranslations('auth.register');
  const tValidation = useTranslations('validation');
  // Rebuild the schema whenever the validation translator identity changes
  // so a runtime locale switch swaps the Russian validation copy with the
  // English one (Phase B1).
  const registerSchema = useMemo(() => createRegisterSchema(tValidation), [tValidation]);

  const {
    register,
    handleSubmit,
    formState: { errors, isSubmitting },
  } = useForm<RegisterInput>({
    resolver: zodResolver(registerSchema),
  });

  const onSubmit = async (data: RegisterInput) => {
    try {
      const res = await api.post(API_PATHS.AUTH.REGISTER, {
        name: data.name,
        email: data.email,
        password: data.password,
      });
      setAuth(res.data.user, res.data.accessToken);
      // Phase 5 / Phase 5 review HIGH-02: drop ALL session-scoped React Query
      // caches before the next render so a fresh actor never observes a prior
      // actor's permissions array. removeQueries is required (not just
      // invalidateQueries): invalidate marks data stale but keeps the
      // cached value, so a PermissionsCacheGuard miss window can briefly
      // surface the previous user's perms. BUSINESS_LIST_QUERY_KEY is
      // ['businesses'] (verified) — partial-prefix sweep also drops nested
      // ['businesses', bizId, 'permissions' | 'roles' | 'members' | …].
      // PERMISSIONS_CATALOG is a separate top-level key — remove it
      // explicitly so a different-deploy catalog can re-fetch.
      queryClient.removeQueries({ queryKey: BUSINESS_LIST_QUERY_KEY });
      queryClient.removeQueries({ queryKey: QUERY_KEYS.PERMISSIONS_CATALOG });
      router.push('/chat');
    } catch (err) {
      const status = (err as { response?: { status?: number } })?.response?.status;
      const message = (err as { response?: { data?: { message?: string } } })?.response?.data
        ?.message;
      if (status === HTTP_STATUS.CONFLICT) {
        toast.error(tReg('emailExists'));
      } else {
        toast.error(message ?? tReg('genericError'));
      }
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

        <Button type="submit" size="lg" className="mt-2 w-full" disabled={isSubmitting}>
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
