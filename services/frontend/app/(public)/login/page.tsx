'use client';

import { useForm } from 'react-hook-form';
import { zodResolver } from '@hookform/resolvers/zod';
import { useRouter } from 'next/navigation';
import Link from 'next/link';
import { toast } from 'sonner';
import { api } from '@/lib/api';
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

  const {
    register,
    handleSubmit,
    formState: { errors, isSubmitting },
  } = useForm<LoginInput>({
    resolver: zodResolver(loginSchema),
  });

  const onSubmit = async (data: LoginInput) => {
    try {
      const res = await api.post('/auth/login', data);
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
            Почта
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
          <div className="flex items-baseline justify-between">
            <Label htmlFor="password" className="text-xs font-medium text-ink-mid">
              Пароль
            </Label>
            {/* TODO(auth): wire forgot-password flow when backend supports it. */}
            <span className="text-xs text-ink-soft">Забыли?</span>
          </div>
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
          Ещё нет аккаунта?{' '}
          <Link href="/register" className="font-medium text-ink hover:underline">
            Зарегистрироваться
          </Link>
        </p>
      </form>
    </AuthShell>
  );
}

function LoginEditorial() {
  return (
    <>
      <MonoLabel>Что нового — апрель</MonoLabel>

      <blockquote className="my-auto">
        <p className="m-0 text-[34px] font-medium leading-[1.2] tracking-[-0.02em] text-ink">
          «Раньше я открывала пять вкладок. Теперь — одну. И посплю наконец.»
        </p>
        <footer className="mt-6 flex items-center gap-3">
          <span className="flex h-9 w-9 items-center justify-center rounded-full border border-line bg-paper-raised text-sm font-semibold text-ink-mid">
            МК
          </span>
          <div>
            <div className="text-sm font-medium text-ink">Мария К.</div>
            <MonoLabel>Салон красоты «Лён» · Санкт-Петербург</MonoLabel>
          </div>
        </footer>
      </blockquote>

      <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
        {[
          { k: 'Среднее время ответа', v: '11 мин', d: 'было 38 мин' },
          { k: 'Каналов в одном ящике', v: '5', d: 'TG, VK, Я.Бизнес' },
        ].map(({ k, v, d }) => (
          <div
            key={k}
            className="rounded-lg border border-line bg-paper-raised p-4"
          >
            <MonoLabel>{k}</MonoLabel>
            <div className="mt-1 text-[28px] font-medium leading-tight tracking-[-0.02em] text-ink">
              {v}
            </div>
            <div className="mt-1 text-xs text-ink-soft">{d}</div>
          </div>
        ))}
      </div>
    </>
  );
}
