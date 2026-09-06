'use client';

// Create-org page. Reached from the BusinessSwitcher CTA («+ Создать
// организацию») and the onboarding card. Posts to /businesses, activates
// the new business in the zustand store, then redirects to /business so
// the user can add logo + hours.
//
// `BusinessRequiredGuard` lists this path in BYPASS_PATHS — without it,
// a user with zero businesses would be bounced to /onboarding by the
// guard's redirect effect, defeating the onboarding → create path.

import { useMemo } from 'react';
import { useRouter } from 'next/navigation';
import { useForm } from 'react-hook-form';
import { zodResolver } from '@hookform/resolvers/zod';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { useTranslations } from 'next-intl';
import { toast } from 'sonner';

import { api } from '@/lib/api';
import { useBusinessStore } from '@/lib/stores/business';
import { BUSINESS_LIST_QUERY_KEY } from '@/lib/hooks/useBusinessList';
import { createBusinessSchema, type BusinessInput } from '@/lib/schemas';
import { Button } from '@/components/ui/button';
import { Field } from '@/components/ui/field';
import { Input } from '@/components/ui/input';
import { MonoLabel } from '@/components/ui/mono-label';
import { PageHeader } from '@/components/ui/page-header';
import { CategoryField } from '@/components/business/CategoryField';
import type { Business } from '@/types/business';

export default function NewBusinessPage() {
  const router = useRouter();
  const qc = useQueryClient();
  const setActive = useBusinessStore((s) => s.setActive);
  const tNewPage = useTranslations('business.newPage');
  const tProfileForm = useTranslations('business.profileForm');
  const tSections = useTranslations('business.sections');
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
      router.push('/business');
    },
    onError: () => toast.error(tNewPage('createError')),
  });

  return (
    <>
      <PageHeader title={tNewPage('title')} sub={tNewPage('sub')} />
      <div className="mx-auto w-full max-w-2xl px-4 pb-10 sm:px-12 sm:pb-16">
        <section className="rounded-lg border border-line bg-paper-raised">
          <header className="border-b border-line-soft px-6 py-4">
            <MonoLabel>{tSections('profile.caption')}</MonoLabel>
            <h2 className="mt-1 text-lg font-medium tracking-tight text-ink">
              {tSections('profile.title')}
            </h2>
          </header>
          <form
            onSubmit={handleSubmit((d) => mutation.mutate(d))}
            className="flex flex-col gap-5 px-6 py-5"
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

              <Field
                htmlFor="phone"
                label={tProfileForm('fields.phone')}
                error={errors.phone?.message}
              >
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
