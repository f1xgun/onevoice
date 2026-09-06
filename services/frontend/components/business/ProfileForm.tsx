'use client';

// Linen rebuild.
// Renders the "Основное" section body (no card chrome, no heading — the page
// wraps each form in a paper-raised section). Save button is rendered by the
// form so the section stays self-contained; the page-level header save lives
// elsewhere and is reserved for cross-section coordination later.

import { useEffect, useMemo, useRef, useState } from 'react';
import { useForm } from 'react-hook-form';
import { zodResolver } from '@hookform/resolvers/zod';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import Image from 'next/image';
import { useTranslations } from 'next-intl';
import { toast } from 'sonner';
import { bizApi } from '@/lib/api/business-api';
import { BIZ_API_PATHS } from '@/lib/constants/bizApiPaths';
import { QUERY_KEYS } from '@/lib/constants/queryKeys';
import { useBusinessStore } from '@/lib/stores/business';
import { createBusinessSchema, type BusinessInput } from '@/lib/schemas';
import { usePermission } from '@/lib/hooks/usePermission';
import { PermissionLoadError } from '@/components/permission/PermissionLoadError';
import { Button } from '@/components/ui/button';
import { Field } from '@/components/ui/field';
import { Input } from '@/components/ui/input';
import { CategoryField } from '@/components/business/CategoryField';
import type { Business } from '@/types/business';

export function ProfileForm({ defaultValues }: { defaultValues?: Partial<Business> }) {
  const tProfileForm = useTranslations('business.profileForm');
  const tCommon = useTranslations('common');
  const tValidation = useTranslations('validation');
  const qc = useQueryClient();
  const fileInputRef = useRef<HTMLInputElement>(null);
  const [logoUrl, setLogoUrl] = useState(defaultValues?.logoUrl ?? '');
  const activeBusinessId = useBusinessStore((s) => s.activeBusinessId);
  const editPerm = usePermission('business.update');
  const canEdit = editPerm.allowed;

  const businessSchema = useMemo(() => createBusinessSchema(tValidation), [tValidation]);

  const {
    register,
    handleSubmit,
    control,
    reset,
    formState: { errors, isSubmitting, isDirty },
  } = useForm<BusinessInput>({
    resolver: zodResolver(businessSchema),
    defaultValues: defaultValues ?? {},
  });

  useEffect(() => {
    if (!defaultValues) return;
    setLogoUrl(defaultValues.logoUrl ?? '');
    if (isDirty) return; // user is mid-edit — don't clobber their typed values
    reset(defaultValues);
  }, [defaultValues, reset, isDirty]);

  const mutation = useMutation({
    mutationFn: (data: BusinessInput) => {
      if (!activeBusinessId) return Promise.reject(new Error('No active business'));
      return bizApi(activeBusinessId).put(BIZ_API_PATHS.BUSINESS.ROOT, data);
    },
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: QUERY_KEYS.BUSINESS_PROFILE(activeBusinessId) });
      toast.success(tProfileForm('saved'));
    },
    onError: () => toast.error(tProfileForm('saveError')),
  });

  const logoMutation = useMutation({
    mutationFn: (file: File) => {
      if (!activeBusinessId) return Promise.reject(new Error('No active business'));
      const formData = new FormData();
      formData.append('logo', file);
      return bizApi(activeBusinessId).put<Business>(BIZ_API_PATHS.BUSINESS.LOGO, formData, {
        headers: { 'Content-Type': 'multipart/form-data' },
      });
    },
    onSuccess: (res) => {
      setLogoUrl(res.data.logoUrl ?? '');
      qc.invalidateQueries({ queryKey: QUERY_KEYS.BUSINESS_PROFILE(activeBusinessId) });
      toast.success(tProfileForm('logoUpdated'));
    },
    onError: () => toast.error(tProfileForm('logoUploadError')),
  });

  function handleFileChange(e: React.ChangeEvent<HTMLInputElement>) {
    const file = e.target.files?.[0];
    if (file) logoMutation.mutate(file);
    e.target.value = '';
  }

  return (
    <form onSubmit={handleSubmit((d) => mutation.mutate(d))} className="flex flex-col gap-5">
      {/* Logo */}
      <div className="flex items-center gap-5">
        <button
          type="button"
          onClick={() => fileInputRef.current?.click()}
          disabled={logoMutation.isPending || !canEdit}
          className="hover:border-ochre/40 relative grid h-20 w-20 shrink-0 place-items-center overflow-hidden rounded-md border border-line bg-paper-sunken text-ink-soft transition-colors hover:text-ink disabled:cursor-not-allowed disabled:opacity-60"
          aria-label={tProfileForm('logoUploadAria')}
        >
          {logoUrl ? (
            <Image
              src={logoUrl}
              alt={tProfileForm('logoAlt')}
              width={80}
              height={80}
              className="h-full w-full object-cover"
              unoptimized
            />
          ) : (
            <span className="font-mono text-[11px] uppercase tracking-[0.04em]">
              {tProfileForm('logoBadge')}
            </span>
          )}
          {logoMutation.isPending && (
            <span className="bg-paper/70 absolute inset-0 grid place-items-center font-mono text-[11px] uppercase tracking-[0.04em] text-ink-soft">
              …
            </span>
          )}
        </button>
        <div className="min-w-0 flex-1">
          <div className="text-sm font-medium text-ink">{tProfileForm('logoLabel')}</div>
          <p className="mt-0.5 text-[13px] text-ink-soft">{tProfileForm('logoHint')}</p>
          <div className="mt-3 flex gap-2">
            <Button
              type="button"
              variant="secondary"
              size="sm"
              onClick={() => fileInputRef.current?.click()}
              disabled={logoMutation.isPending || !canEdit}
            >
              {tProfileForm('uploadLogo')}
            </Button>
            {logoUrl && (
              <Button
                type="button"
                variant="ghost"
                size="sm"
                onClick={() => setLogoUrl('')}
                disabled={logoMutation.isPending || !canEdit}
              >
                {tProfileForm('removeLogo')}
              </Button>
            )}
          </div>
        </div>
        <input
          ref={fileInputRef}
          type="file"
          accept="image/jpeg,image/png,image/webp,image/gif"
          className="hidden"
          onChange={handleFileChange}
          disabled={!canEdit}
        />
      </div>

      <div className="grid grid-cols-1 gap-4 md:grid-cols-2">
        <Field
          htmlFor="name"
          label={tProfileForm('fields.name')}
          required
          error={errors.name?.message}
        >
          <Input id="name" {...register('name')} placeholder={tProfileForm('namePlaceholder')} />
        </Field>

        <CategoryField control={control} error={errors.category?.message} />

        <Field
          htmlFor="address"
          label={tProfileForm('fields.address')}
          error={errors.address?.message}
          className="md:col-span-2"
        >
          <Input
            id="address"
            {...register('address')}
            placeholder={tProfileForm('addressPlaceholder')}
          />
        </Field>

        <Field htmlFor="phone" label={tProfileForm('fields.phone')} error={errors.phone?.message}>
          <Input id="phone" {...register('phone')} placeholder="+79001234567" />
        </Field>

        <Field
          htmlFor="website"
          label={tProfileForm('fields.website')}
          error={errors.website?.message}
        >
          <Input id="website" {...register('website')} placeholder="https://example.com" />
        </Field>

        <Field
          htmlFor="description"
          label={tProfileForm('fields.description')}
          error={errors.description?.message}
          hint={tProfileForm('descriptionHint')}
          className="md:col-span-2"
        >
          <textarea
            id="description"
            {...register('description')}
            rows={4}
            placeholder={tProfileForm('descriptionPlaceholder')}
            className="focus:ring-ochre/20 flex w-full rounded-md border border-line bg-paper-raised px-3 py-2 text-sm text-ink transition-[border-color,box-shadow] duration-150 placeholder:text-ink-soft focus:border-ochre focus:outline-none focus:ring-2 disabled:cursor-not-allowed disabled:opacity-50"
          />
        </Field>
      </div>

      {editPerm.isError && <PermissionLoadError onRetry={editPerm.refetch} />}

      <div className="flex items-center justify-end pt-1">
        <Button
          type="submit"
          variant="primary"
          size="md"
          disabled={isSubmitting || mutation.isPending || !isDirty || !canEdit}
        >
          {isSubmitting || mutation.isPending ? tProfileForm('saving') : tCommon('save')}
        </Button>
      </div>
    </form>
  );
}
