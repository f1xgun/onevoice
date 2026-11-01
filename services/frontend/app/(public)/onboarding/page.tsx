'use client';

import { useState } from 'react';
import { FirstLaunch } from '@/components/onboarding/FirstLaunch';
import { useRouter } from 'next/navigation';
import { useForm } from 'react-hook-form';
import { zodResolver } from '@hookform/resolvers/zod';
import { z } from 'zod';
import { useTranslations } from 'next-intl';
import { Link2, LogOut } from 'lucide-react';

import { ActionButton as Button } from '@/components/design-system/ActionButton';
import { AppInput as Input } from '@/components/design-system/AppInput';
import { Label } from '@/components/ui/label';
import { AppearanceControls } from '@/components/design-system/AppearanceControls';
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
  const [showInvite, setShowInvite] = useState(false);
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
    <div className="min-h-screen bg-paper py-group">
      <div className="mx-auto w-full max-w-2xl">
        <div className="mb-group flex items-center justify-end gap-1 px-gutter md:px-gutter-desktop">
          <AppearanceControls />
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
        <h1 className="sr-only">{tOnboarding('title')}</h1>
        <FirstLaunch />
        <div className="px-gutter md:px-gutter-desktop">
          <Button variant="ghost" onClick={() => setShowInvite(!showInvite)}>
            {tHaveInvite('title')}
          </Button>
          {showInvite && (
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
          )}
        </div>
      </div>
    </div>
  );
}
