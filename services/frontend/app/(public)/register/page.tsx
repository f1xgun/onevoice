'use client';

import { useForm } from 'react-hook-form';
import { zodResolver } from '@hookform/resolvers/zod';
import { useRouter } from 'next/navigation';
import Link from 'next/link';
import { toast } from 'sonner';
import { api } from '@/lib/api';
import { useAuthStore } from '@/lib/auth';
import { registerSchema, type RegisterInput } from '@/lib/schemas';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { AuthShell } from '@/components/auth/AuthShell';
import { MonoLabel } from '@/components/ui/mono-label';

export default function RegisterPage() {
  const router = useRouter();
  const setAuth = useAuthStore((s) => s.setAuth);

  const {
    register,
    handleSubmit,
    formState: { errors, isSubmitting },
  } = useForm<RegisterInput>({
    resolver: zodResolver(registerSchema),
  });

  const onSubmit = async (data: RegisterInput) => {
    try {
      const res = await api.post('/auth/register', {
        name: data.name,
        email: data.email,
        password: data.password,
      });
      setAuth(res.data.user, res.data.accessToken);
      router.push('/chat');
    } catch (err) {
      const status = (err as { response?: { status?: number } })?.response?.status;
      const message = (err as { response?: { data?: { message?: string } } })?.response?.data
        ?.message;
      if (status === 409) {
        toast.error('Пользователь с таким email уже существует');
      } else {
        toast.error(message ?? 'Ошибка регистрации. Проверьте данные.');
      }
    }
  };

  return (
    <AuthShell
      eyebrow="Создание аккаунта"
      title="Начнём знакомство."
      description="Минута на регистрацию — потом подключим каналы и поговорим о голосе бизнеса."
      aside={<RegisterEditorial />}
    >
      <form onSubmit={handleSubmit(onSubmit)} className="flex flex-col gap-4">
        <div className="flex flex-col gap-1.5">
          <Label htmlFor="name" className="text-xs font-medium text-ink-mid">
            Как к вам обращаться
          </Label>
          <Input id="name" placeholder="Алина" autoComplete="given-name" {...register('name')} />
          {errors.name && <p className="text-sm text-[var(--ov-danger)]">{errors.name.message}</p>}
        </div>

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

        <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
          <div className="flex flex-col gap-1.5">
            <Label htmlFor="password" className="text-xs font-medium text-ink-mid">
              Пароль
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
              Ещё раз
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
          {isSubmitting ? 'Создаём аккаунт…' : 'Создать аккаунт'}
        </Button>

        <p className="mt-6 text-sm text-ink-soft">
          Уже зарегистрированы?{' '}
          <Link href="/login" className="font-medium text-ink hover:underline">
            Войти
          </Link>
        </p>
      </form>
    </AuthShell>
  );
}

function RegisterEditorial() {
  return (
    <>
      <MonoLabel>Что вы получите</MonoLabel>

      <div className="my-auto flex flex-col gap-4">
        {[
          {
            title: 'Один разговор — все каналы',
            body: 'Telegram, ВКонтакте, Яндекс.Бизнес отвечают по одной команде, без переключения между вкладками.',
          },
          {
            title: 'AI говорит вашим голосом',
            body: 'Опишете тон один раз — на «Здравствуйте, у нас тихо в среду» он не ответит «Хей! 👋».',
          },
          {
            title: 'Спокойнее по утрам',
            body: 'Ничего не пропустите: AI готовит черновики, вы пьёте кофе и подтверждаете в один клик.',
          },
        ].map(({ title, body }) => (
          <div key={title} className="rounded-lg border border-line bg-paper-raised p-4">
            <div className="text-base font-medium leading-tight tracking-[-0.005em] text-ink">
              {title}
            </div>
            <p className="mt-1.5 text-sm leading-relaxed text-ink-mid">{body}</p>
          </div>
        ))}
      </div>

      <div className="rounded-md border border-line-soft bg-paper-raised p-4 text-sm leading-relaxed text-ink-mid">
        После регистрации подключим каналы (Telegram, VK, Яндекс…). Можно пройти всё за пару минут.
      </div>
    </>
  );
}
