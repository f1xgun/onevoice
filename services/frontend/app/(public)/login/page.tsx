'use client';

import { useForm } from 'react-hook-form';
import { zodResolver } from '@hookform/resolvers/zod';
import { useRouter } from 'next/navigation';
import Link from 'next/link';
import { useTranslations } from 'next-intl';
import { toast } from 'sonner';
import { api } from '@/lib/api';
import { API_PATHS } from '@/lib/constants/apiPaths';
import { useAuthStore } from '@/lib/auth';
import { loginSchema, type LoginInput } from '@/lib/schemas';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { AuthShell } from '@/components/auth/AuthShell';
import { MonoLabel } from '@/components/ui/mono-label';

export default function LoginPage() {
  const router = useRouter();
  const setAuth = useAuthStore((s) => s.setAuth);
  const tLogin = useTranslations('auth.login');

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
      router.push('/chat');
    } catch (err) {
      const message = (err as { response?: { data?: { message?: string } } })?.response?.data
        ?.message;
      toast.error(message ?? 'Неверный email или пароль');
    }
  };

  return (
    <AuthShell
      eyebrow="Вход"
      title="С возвращением."
      description="Введите почту и пароль, чтобы открыть общий ящик."
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
          {isSubmitting ? 'Входим…' : 'Войти'}
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
