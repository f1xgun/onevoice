'use client';

import { useMemo } from 'react';
import { useRouter } from 'next/navigation';
import { useForm } from 'react-hook-form';
import { zodResolver } from '@hookform/resolvers/zod';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { useTranslations } from 'next-intl';
import { toast } from 'sonner';

import { ProRequest } from '@/components/billing/ProRequest';
import { isBusinessPlanLimitError } from '@/lib/resolveErrorMap';
import { api } from '@/lib/api';
import { useBusinessStore } from '@/lib/stores/business';
import { BUSINESS_LIST_QUERY_KEY } from '@/lib/hooks/useBusinessList';
import { createBusinessSchema, type BusinessInput } from '@/lib/schemas';
import { ActionButton as Button } from '@/components/design-system/ActionButton';
import { Field } from '@/components/ui/field';
import { AppInput as Input } from '@/components/design-system/AppInput';
import { CategoryField } from '@/components/business/CategoryField';
import type { Business } from '@/types/business';

interface PointStepProps {
  onCreated: (businessId: string) => void;
}

export function PointStep({ onCreated }: PointStepProps) {
  const router = useRouter();
  const qc = useQueryClient();
  const setActive = useBusinessStore((s) => s.setActive);
  const tLaunch = useTranslations('launchV2');
  const tNewPage = useTranslations('business.newPage');
  const tProfileForm = useTranslations('business.profileForm');
  const tValidation = useTranslations('validation');
  const tCommon = useTranslations('common');

  const schema = useMemo(() => createBusinessSchema(tValidation), [tValidation]);

  const {
    register,
    handleSubmit,
    control,
    formState: { errors, isSubmitting },
  } = useForm<BusinessInput>({
    resolver: zodResolver(schema),
    defaultValues: {
      name: '',
      category: '',
      address: '',
      phone: '',
      website: '',
      description: '',
    },
  });

  const mutation = useMutation({
    mutationFn: async (data: BusinessInput) => {
      const payload = {
        name: data.name,
        category: data.category,
        address: data.address ?? '',
        phone: data.phone ?? '',
        website: data.website ? data.website : null,
        description: data.description ?? '',
      };
      const res = await api.post<Business>('/businesses', payload);
      return res.data;
    },
    onSuccess: (created) => {
      setActive(created.id);
      qc.invalidateQueries({ queryKey: BUSINESS_LIST_QUERY_KEY });
      toast.success(tNewPage('createdToast', { name: created.name }));
      onCreated(created.id);
    },
    onError: (error) => {
      if (!isBusinessPlanLimitError(error)) toast.error(tNewPage('createError'));
    },
  });

  return (
    <>
      <header className="pb-group pt-group">
        <h1 className="text-page-title">{tLaunch('pointTitle')}</h1>
        <p className="mt-1 text-ink-mid">{tLaunch('pointHint')}</p>
      </header>
      <div className="w-full">
        <section className="rounded-block bg-card">
          {isBusinessPlanLimitError(mutation.error) && (
            <div role="alert" className="space-y-3 rounded-md border border-line p-4">
              <p>{tNewPage('createError')}</p>
              <ProRequest source="business-limit" />
            </div>
          )}
          <form
            onSubmit={handleSubmit((d) => mutation.mutate(d))}
            className="flex flex-col gap-5 p-4"
          >
            <div className="grid grid-cols-1 gap-4 md:grid-cols-2">
              <Field
                htmlFor="name"
                label={tProfileForm('fields.name')}
                required
                error={errors.name?.message}
              >
                <Input
                  id="name"
                  {...register('name')}
                  placeholder={tProfileForm('namePlaceholder')}
                />
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
            </div>

            <div className="flex items-center justify-end gap-2 pt-1">
              <Button type="button" variant="ghost" size="md" onClick={() => router.back()}>
                {tCommon('cancel')}
              </Button>
              <Button
                type="submit"
                variant="primary"
                size="md"
                disabled={isSubmitting || mutation.isPending}
              >
                {isSubmitting || mutation.isPending ? tNewPage('submitting') : tNewPage('submit')}
              </Button>
            </div>
          </form>
        </section>
      </div>
    </>
  );
}
