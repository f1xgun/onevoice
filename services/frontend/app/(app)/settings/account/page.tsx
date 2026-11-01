'use client';

import { useMemo, useState } from 'react';
import { Check, AlertTriangle } from 'lucide-react';
import { useForm } from 'react-hook-form';
import { zodResolver } from '@hookform/resolvers/zod';
import { z } from 'zod';
import { useMutation } from '@tanstack/react-query';
import { useTranslations } from 'next-intl';
import { toast } from 'sonner';
import { useAuthStore } from '@/lib/auth';
import { api } from '@/lib/api';
import { API_PATHS } from '@/lib/constants/apiPaths';
import { ActionButton as Button } from '@/components/design-system/ActionButton';
import { AppInput as Input } from '@/components/design-system/AppInput';
import { StatusLine } from '@/components/design-system/StatusLine';
import { Label } from '@/components/ui/label';
import { DeleteConfirmModal } from '@/components/account/DeleteConfirmModal';

const FIELD_ERROR_IDS = {
  currentPassword: 'currentPassword-error',
  newPassword: 'newPassword-error',
  confirmPassword: 'confirmPassword-error',
  name: 'account-name-error',
};

const NEW_PASSWORD_MIN_LEN = 8;
// Mirror the backend UpdateProfileRequest constraint (min=2,max=100).
const NAME_MIN_LEN = 2;
const NAME_MAX_LEN = 100;

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
  const [confirmOpen, setConfirmOpen] = useState(false);
  const tDelete = useTranslations('account.deletion');
  const tV2 = useTranslations('settings.v2');

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
    <div className="mx-auto max-w-[816px] px-gutter pb-10 pt-5 md:px-gutter-desktop">
      <h1 className="mb-title-gap text-page-title">{tV2('account')}</h1>

      <div className="space-y-group">
        <div className="flex flex-col gap-group">
          {/* Account */}
          <section className="rounded-block bg-block p-block md:p-block-desktop">
            <div className="flex flex-col gap-4">
              <AccountNameForm />
            </div>
          </section>

          {/* Password */}
          <section>
            <h2 className="mb-block-gap text-group-title">{tV2('signIn')}</h2>
            <details className="rounded-block bg-block p-block md:p-block-desktop">
              <summary className="min-h-tap cursor-pointer text-reading">
                {tSettings('securityTitle')}
              </summary>
              <form
                onChange={() => {
                  if (!mutation.isPending) mutation.reset();
                }}
                onSubmit={handleSubmit((d) => mutation.mutate(d))}
                className="mt-4 flex flex-col gap-4"
              >
                <div className="flex flex-col gap-1.5">
                  <Label htmlFor="currentPassword" className="text-meta font-medium text-ink">
                    {tSettings('currentPassword')}
                  </Label>
                  <Input
                    disabled={mutation.isPending}
                    id="currentPassword"
                    aria-invalid={!!errors.currentPassword}
                    aria-describedby={
                      errors.currentPassword ? FIELD_ERROR_IDS.currentPassword : undefined
                    }
                    type="password"
                    autoComplete="current-password"
                    {...register('currentPassword')}
                  />
                  {errors.currentPassword && (
                    <p id="currentPassword-error" role="alert" className="text-meta text-danger">
                      {errors.currentPassword.message}
                    </p>
                  )}
                </div>

                <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
                  <div className="flex flex-col gap-1.5">
                    <Label htmlFor="newPassword" className="text-meta font-medium text-ink">
                      {tSettings('newPassword')}
                    </Label>
                    <Input
                      disabled={mutation.isPending}
                      id="newPassword"
                      aria-invalid={!!errors.newPassword}
                      aria-describedby={
                        errors.newPassword ? FIELD_ERROR_IDS.newPassword : undefined
                      }
                      type="password"
                      autoComplete="new-password"
                      {...register('newPassword')}
                    />
                    {errors.newPassword && (
                      <p id="newPassword-error" role="alert" className="text-meta text-danger">
                        {errors.newPassword.message}
                      </p>
                    )}
                  </div>
                  <div className="flex flex-col gap-1.5">
                    <Label htmlFor="confirmPassword" className="text-meta font-medium text-ink">
                      {tSettings('confirmPassword')}
                    </Label>
                    <Input
                      disabled={mutation.isPending}
                      id="confirmPassword"
                      aria-invalid={!!errors.confirmPassword}
                      aria-describedby={
                        errors.confirmPassword ? FIELD_ERROR_IDS.confirmPassword : undefined
                      }
                      type="password"
                      autoComplete="new-password"
                      {...register('confirmPassword')}
                    />
                    {errors.confirmPassword && (
                      <p id="confirmPassword-error" role="alert" className="text-meta text-danger">
                        {errors.confirmPassword.message}
                      </p>
                    )}
                  </div>
                </div>

                {mutation.isSuccess && (
                  <StatusLine
                    role="status"
                    tone="success"
                    icon={Check}
                    text={tSettings('passwordChanged')}
                  />
                )}
                {mutation.isError && (
                  <StatusLine
                    role="alert"
                    tone="danger"
                    icon={AlertTriangle}
                    text={tSettings('passwordChangeError')}
                  />
                )}
                <div>
                  <Button type="submit" disabled={isSubmitting || mutation.isPending}>
                    {isSubmitting || mutation.isPending
                      ? tSettings('submitting')
                      : tSettings('submit')}
                  </Button>
                </div>
              </form>
            </details>
          </section>
        </div>

        <section>
          <h2 className="text-group-title">{tDelete('deleteButton')}</h2>
          <p className="mt-1 text-[15px] leading-[22px] text-ink-soft">
            {tDelete('dangerZoneDescription')}
          </p>
          <Button variant="danger" className="mt-4" onClick={() => setConfirmOpen(true)}>
            {tDelete('deleteButton')}
          </Button>
        </section>
        <DeleteConfirmModal open={confirmOpen} onOpenChange={setConfirmOpen} />
      </div>
    </div>
  );
}

function ReadOnlyField({ label, value, mono }: { label: string; value?: string; mono?: boolean }) {
  const tSettings = useTranslations('settings.page');
  return (
    <div className="flex flex-col gap-1">
      <span className="text-meta text-ink-soft">{label}</span>
      <div className={mono ? 'font-mono text-sm text-ink' : 'text-sm text-ink'}>
        {value ?? tSettings('fallbackEmpty')}
      </div>
    </div>
  );
}

type NameInput = { name: string };

// Editable display name. `values` (not defaultValues) re-syncs the field when
// the user object hydrates from /auth/me after first paint. On success the
// auth store is patched so the new name shows everywhere without a reload.
function AccountNameForm() {
  const tSettings = useTranslations('settings.page');
  const tV2 = useTranslations('settings.v2');
  const user = useAuthStore((s) => s.user);
  const accessToken = useAuthStore((s) => s.accessToken);
  const setAuth = useAuthStore((s) => s.setAuth);

  const schema = useMemo(
    () =>
      z.object({
        name: z
          .string()
          .trim()
          .min(NAME_MIN_LEN, tSettings('nameMinChars'))
          .max(NAME_MAX_LEN, tSettings('nameMaxChars')),
      }),
    [tSettings]
  );

  const {
    register,
    handleSubmit,
    reset,
    formState: { errors, isSubmitting, isDirty },
  } = useForm<NameInput>({
    resolver: zodResolver(schema),
    values: { name: user?.name ?? '' },
  });

  const mutation = useMutation({
    mutationFn: (data: NameInput) => api.patch(API_PATHS.AUTH.PROFILE, { name: data.name.trim() }),
    onSuccess: (_res, data) => {
      const name = data.name.trim();
      if (user && accessToken) setAuth({ ...user, name }, accessToken);
      toast.success(tSettings('nameSaved'));
      reset({ name });
    },
    onError: () => toast.error(tSettings('nameSaveError')),
  });

  const busy = isSubmitting || mutation.isPending;

  return (
    <form
      onChange={() => {
        if (!mutation.isPending) mutation.reset();
      }}
      onSubmit={handleSubmit((d) => mutation.mutate(d))}
      className="flex flex-col gap-1.5"
    >
      <Label htmlFor="account-name">
        <span className="text-meta text-ink-soft">{tSettings('nameLabel')}</span>
      </Label>
      <div className="flex flex-col gap-3">
        <Input
          disabled={mutation.isPending}
          id="account-name"
          aria-invalid={!!errors.name}
          aria-describedby={errors.name ? FIELD_ERROR_IDS.name : undefined}
          autoComplete="name"
          placeholder={tSettings('namePlaceholder')}
          className="min-w-0 flex-1"
          {...register('name')}
        />
        <p className="text-meta text-ink-soft">{tV2('accountNameHint')}</p>
        <ReadOnlyField label={tV2('loginEmail')} value={user?.email} />
        <Button type="submit" disabled={!isDirty || busy}>
          {busy ? tSettings('submitting') : tSettings('saveName')}
        </Button>
      </div>
      {mutation.isSuccess && (
        <StatusLine role="status" tone="success" icon={Check} text={tSettings('nameSaved')} />
      )}
      {mutation.isError && (
        <StatusLine
          role="alert"
          tone="danger"
          icon={AlertTriangle}
          text={tSettings('nameSaveError')}
        />
      )}
      {errors.name && (
        <p id="account-name-error" role="alert" className="text-meta text-danger">
          {errors.name.message}
        </p>
      )}
    </form>
  );
}
