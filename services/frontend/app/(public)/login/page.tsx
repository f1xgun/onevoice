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

export default function LoginPage() {
  const router = useRouter();
  const setAuth = useAuthStore((s) => s.setAuth);
  const tLogin = useTranslations('auth.login');
  const tValidation = useTranslations('validation');
  // Rebuild the schema whenever the validation translator identity changes
  // so a runtime locale switch swaps the Russian validation copy with the
  // English one (Phase B1).
  const loginSchema = useMemo(() => createLoginSchema(tValidation), [tValidation]);

  const {
    register,
    handleSubmit,
    formState: { errors, isSubmitting },
  } = useForm<LoginInput>({
    resolver: zodResolver(loginSchema),
  });

  const onSubmit = async (data: LoginInput) => {
    try {
      const res = await api.post(API_PATHS.AUTH.LOGIN, data);
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
      const message = (err as { response?: { data?: { message?: string } } })?.response?.data
        ?.message;
      toast.error(message ?? tLogin('invalidCredentials'));
    }
  };

  return (
    <AuthShell
      eyebrow={tLogin('eyebrow')}
      title={tLogin('title')}
      description={tLogin('description')}
      aside={<LoginEditorial />}
    >
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
