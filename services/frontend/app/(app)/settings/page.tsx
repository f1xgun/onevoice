'use client';

import { useMemo } from 'react';
import Link from 'next/link';
import { ChevronRight, ShieldCheck } from 'lucide-react';
import { useForm } from 'react-hook-form';
import { zodResolver } from '@hookform/resolvers/zod';
import { z } from 'zod';
import { useMutation } from '@tanstack/react-query';
import { useTranslations } from 'next-intl';
import { toast } from 'sonner';
import { useAuthStore } from '@/lib/auth';
import { api } from '@/lib/api';
import { API_PATHS } from '@/lib/constants/apiPaths';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { PageHeader } from '@/components/ui/page-header';
import { MonoLabel } from '@/components/ui/mono-label';

const NEW_PASSWORD_MIN_LEN = 8;

// Schema is built inside the component so validation messages follow the
// active locale. useMemo keeps schema identity stable across re-renders
// for react-hook-form's `resolver` stability.
type PasswordInput = {
  currentPassword: string;
  newPassword: string;
  confirmPassword: string;
};

export default function SettingsPage() {
  const tSettings = useTranslations('settings.page');
  const user = useAuthStore((s) => s.user);

  const passwordSchema = useMemo(
    () =>
      z
        .object({
          currentPassword: z.string().min(1, tSettings('currentPasswordRequired')),
          newPassword: z.string().min(NEW_PASSWORD_MIN_LEN, tSettings('newPasswordMinChars')),
          confirmPassword: z.string(),
        })
        .refine((d) => d.newPassword === d.confirmPassword, {
          message: tSettings('passwordsMismatch'),
          path: ['confirmPassword'],
        }),
    [tSettings]
  );

  const {
    register,
    handleSubmit,
    reset,
    formState: { errors, isSubmitting },
  } = useForm<PasswordInput>({
    resolver: zodResolver(passwordSchema),
  });

  const mutation = useMutation({
    mutationFn: (data: PasswordInput) =>
      api.put(API_PATHS.AUTH.PASSWORD, {
        currentPassword: data.currentPassword,
        newPassword: data.newPassword,
      }),
    onSuccess: () => {
      toast.success(tSettings('passwordChanged'));
      reset();
    },
    onError: () => toast.error(tSettings('passwordChangeError')),
  });

  return (
    <>
      <PageHeader title={tSettings('title')} sub={tSettings('sub')} />

      <div className="grid grid-cols-1 gap-8 px-4 pb-10 sm:px-12 sm:pb-16 lg:grid-cols-[1fr_320px]">
        <div className="flex flex-col gap-6">
          {/* Account */}
          <section className="rounded-lg border border-line bg-paper-raised">
            <header className="border-b border-line-soft px-6 py-4">
              <MonoLabel>{tSettings('accountKicker')}</MonoLabel>
              <h2 className="mt-1 text-lg font-medium tracking-tight text-ink">
                {tSettings('accountTitle')}
              </h2>
            </header>
            <div className="grid grid-cols-1 gap-4 px-6 py-5 sm:grid-cols-2">
              <ReadOnlyField label={tSettings('nameLabel')} value={user?.name} />
              <ReadOnlyField label={tSettings('emailLabel')} value={user?.email} />
            </div>
          </section>

          {/* Password */}
          <section className="rounded-lg border border-line bg-paper-raised">
            <header className="border-b border-line-soft px-6 py-4">
              <MonoLabel>{tSettings('securityKicker')}</MonoLabel>
              <h2 className="mt-1 text-lg font-medium tracking-tight text-ink">
                {tSettings('securityTitle')}
              </h2>
            </header>
            <form
              onSubmit={handleSubmit((d) => mutation.mutate(d))}
              className="flex flex-col gap-4 px-6 py-5"
            >
              <div className="flex flex-col gap-1.5">
                <Label htmlFor="currentPassword" className="text-xs font-medium text-ink-mid">
                  {tSettings('currentPassword')}
                </Label>
                <Input
                  id="currentPassword"
                  type="password"
                  autoComplete="current-password"
                  {...register('currentPassword')}
                />
                {errors.currentPassword && (
                  <p className="text-sm text-[var(--ov-danger)]">
                    {errors.currentPassword.message}
                  </p>
                )}
              </div>

              <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
                <div className="flex flex-col gap-1.5">
                  <Label htmlFor="newPassword" className="text-xs font-medium text-ink-mid">
                    {tSettings('newPassword')}
                  </Label>
                  <Input
                    id="newPassword"
                    type="password"
                    autoComplete="new-password"
                    {...register('newPassword')}
                  />
                  {errors.newPassword && (
                    <p className="text-sm text-[var(--ov-danger)]">{errors.newPassword.message}</p>
                  )}
                </div>
                <div className="flex flex-col gap-1.5">
                  <Label htmlFor="confirmPassword" className="text-xs font-medium text-ink-mid">
                    {tSettings('confirmPassword')}
                  </Label>
                  <Input
                    id="confirmPassword"
                    type="password"
                    autoComplete="new-password"
                    {...register('confirmPassword')}
                  />
                  {errors.confirmPassword && (
                    <p className="text-sm text-[var(--ov-danger)]">
                      {errors.confirmPassword.message}
                    </p>
                  )}
                </div>
              </div>

              <div>
                <Button type="submit" disabled={isSubmitting || mutation.isPending}>
                  {isSubmitting || mutation.isPending
                    ? tSettings('submitting')
                    : tSettings('submit')}
                </Button>
              </div>
            </form>
          </section>
        </div>

        {/* Right rail */}
        <aside
          aria-labelledby="settings-rail-label"
          className="flex flex-col gap-3 lg:sticky lg:top-8 lg:self-start"
        >
          <MonoLabel id="settings-rail-label" className="px-1">
            {tSettings('sectionsLabel')}
          </MonoLabel>
          <RailTile
            href="/settings/tools"
            icon={<ShieldCheck size={18} aria-hidden />}
            title={tSettings('rail.toolsTitle')}
            description={tSettings('rail.toolsDescription')}
          />
        </aside>
      </div>
    </>
  );
}

function ReadOnlyField({ label, value, mono }: { label: string; value?: string; mono?: boolean }) {
  const tSettings = useTranslations('settings.page');
  return (
    <div className="flex flex-col gap-1">
      <MonoLabel>{label}</MonoLabel>
      <div className={mono ? 'font-mono text-sm text-ink' : 'text-sm text-ink'}>
        {value ?? tSettings('fallbackEmpty')}
      </div>
    </div>
  );
}

function RailTile({
  href,
  icon,
  title,
  description,
}: {
  href: string;
  icon: React.ReactNode;
  title: string;
  description: string;
}) {
  return (
    <Link
      href={href}
      className="hover:border-ochre/40 group flex items-start gap-3 rounded-lg border border-line bg-paper-raised p-4 transition-colors hover:bg-paper-sunken"
    >
      <span className="mt-0.5 shrink-0 text-ink-soft group-hover:text-ink">{icon}</span>
      <div className="min-w-0 flex-1">
        <div className="text-sm font-medium text-ink">{title}</div>
        <p className="mt-1 text-xs leading-relaxed text-ink-mid">{description}</p>
      </div>
      <ChevronRight
        size={16}
        aria-hidden
        className="mt-1 shrink-0 text-ink-faint transition-transform group-hover:translate-x-0.5 group-hover:text-ink-soft"
      />
    </Link>
  );
}
