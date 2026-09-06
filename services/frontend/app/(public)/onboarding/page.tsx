'use client';

import Link from 'next/link';
import { useRouter } from 'next/navigation';
import { useForm } from 'react-hook-form';
import { zodResolver } from '@hookform/resolvers/zod';
import { z } from 'zod';
import { useTranslations } from 'next-intl';
import { Plus, Link2, LogOut } from 'lucide-react';

import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { LanguageSwitcher } from '@/components/ui/LanguageSwitcher';
import { useLogout } from '@/lib/hooks/useLogout';

const TOKEN_REGEX = /^[A-Za-z0-9_-]{43}$/;

interface PasteTokenForm {
  token: string;
}

function makeSchema(invalidMessage: string) {
  return z.object({
    token: z.string().regex(TOKEN_REGEX, invalidMessage),
  });
}

export default function OnboardingPage() {
  const router = useRouter();
  const tOnboarding = useTranslations('onboarding');
  const tCreateOrg = useTranslations('onboarding.createOrg');
  const tHaveInvite = useTranslations('onboarding.haveInvite');
  const tNav = useTranslations('nav');
  const logout = useLogout();

  const schema = makeSchema(tHaveInvite('errors.invalidFormat'));

  const {
    register,
    handleSubmit,
    formState: { errors, isSubmitting },
  } = useForm<PasteTokenForm>({
    resolver: zodResolver(schema),
  });

  const onPasteSubmit = (data: PasteTokenForm) => {
    router.push(`/invite/${data.token}`);
  };

  async function handleLogout() {
    await logout();
    router.replace('/login');
  }

  return (
    <div className="flex min-h-screen items-center justify-center bg-paper px-4 py-12">
      <div className="w-full max-w-2xl">
        <div className="mb-4 flex items-center justify-end gap-1">
          <LanguageSwitcher />
          <Button
            type="button"
            variant="ghost"
            size="sm"
            onClick={() => void handleLogout()}
            className="gap-2 text-ink-mid"
          >
            <LogOut size={16} aria-hidden />
            {tNav('logout')}
          </Button>
        </div>
        <header className="mb-6 text-center">
          <h1 className="text-2xl font-medium tracking-tight text-ink">{tOnboarding('title')}</h1>
          <p className="mt-2 text-sm text-ink-mid">{tOnboarding('sub')}</p>
        </header>
        <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
          <Link
            href="/business/new"
            className="group flex flex-col gap-3 rounded-lg border border-line bg-paper-raised p-6 transition-colors hover:bg-paper-sunken"
          >
            <Plus size={20} className="text-ink-mid group-hover:text-ink" aria-hidden />
            <h2 className="text-base font-medium text-ink">{tCreateOrg('title')}</h2>
            <p className="text-sm text-ink-mid">{tCreateOrg('body')}</p>
            <span className="mt-auto text-sm font-medium text-ink-mid group-hover:text-ink">
              {tCreateOrg('cta')} →
            </span>
          </Link>

          <form
            onSubmit={handleSubmit(onPasteSubmit)}
            className="flex flex-col gap-3 rounded-lg border border-line bg-paper-raised p-6"
          >
            <Link2 size={20} className="text-ink-mid" aria-hidden />
            <h2 className="text-base font-medium text-ink">{tHaveInvite('title')}</h2>
            <p className="text-sm text-ink-mid">{tHaveInvite('body')}</p>
            <div className="mt-2 flex flex-col gap-1.5">
              <Label htmlFor="invite-token" className="text-xs font-medium text-ink-mid">
                {tHaveInvite('label')}
              </Label>
              <Input
                id="invite-token"
                placeholder={tHaveInvite('placeholder')}
                {...register('token')}
                aria-invalid={!!errors.token}
              />
              {errors.token && (
                <p className="text-sm text-danger" role="alert">
                  {errors.token.message}
                </p>
              )}
            </div>
            <Button type="submit" disabled={isSubmitting} className="mt-2">
              {tHaveInvite('cta')}
            </Button>
          </form>
        </div>
      </div>
    </div>
  );
}
