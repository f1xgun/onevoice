'use client';

import { LoadingPlaceholder } from '@/components/states/LoadingPlaceholder';

import { useEffect } from 'react';
import { zodResolver } from '@hookform/resolvers/zod';
import { useLocale, useTranslations } from 'next-intl';
import { useForm } from 'react-hook-form';
import { toast } from 'sonner';
import { z } from 'zod';

import { ActionButton as Button } from '@/components/design-system/ActionButton';
import { Switch } from '@/components/ui/switch';
import { useDriftAlerts, useUpdateDriftAlerts } from '@/lib/hooks/useDriftAlerts';
import { usePermission } from '@/lib/hooks/usePermission';
import { useBusinessStore } from '@/lib/stores/business';

const schema = z.object({ enabled: z.boolean(), locale: z.enum(['ru', 'en']) });
type Values = z.infer<typeof schema>;

export function DriftAlertSettings({ businessId }: { businessId: string }) {
  const t = useTranslations('integrations.sync.alerts');
  const uiLocale = useLocale() === 'en' ? 'en' : 'ru';
  const activeBusinessId = useBusinessStore((state) => state.activeBusinessId);
  const readPermission = usePermission('business.read');
  const updatePermission = usePermission('business.update');
  const canRead = readPermission.allowed;
  const canUpdate = updatePermission.allowed;
  const query = useDriftAlerts(businessId, canRead && activeBusinessId === businessId);
  const mutation = useUpdateDriftAlerts();
  const { register, handleSubmit, reset, watch, setValue } = useForm<Values>({
    resolver: zodResolver(schema),
    defaultValues: { enabled: false, locale: uiLocale },
  });
  useEffect(() => {
    if (query.data && activeBusinessId === businessId) reset(query.data);
  }, [activeBusinessId, businessId, query.data, reset]);
  const enabled = watch('enabled');
  const submit = handleSubmit((values) =>
    mutation.mutate(
      { businessId, settings: values },
      {
        onSuccess: () => {
          if (useBusinessStore.getState().activeBusinessId === businessId)
            toast.success(t('saved'));
        },
        onError: () => {
          if (useBusinessStore.getState().activeBusinessId === businessId) toast.error(t('failed'));
        },
      }
    )
  );

  if (readPermission.isLoading) {
    return (
      <LoadingPlaceholder
        className="mt-6 border-t border-line-soft pt-5 text-sm text-ink-mid"
        role="status"
      >
        {t('loading')}
      </LoadingPlaceholder>
    );
  }
  if (readPermission.isError) {
    return (
      <div className="mt-6 border-t border-line-soft pt-5">
        <p className="text-sm text-ink-mid">{t('loadFailed')}</p>
        <Button
          type="button"
          variant="secondary"
          size="sm"
          className="mt-3"
          onClick={() => readPermission.refetch()}
        >
          {t('retry')}
        </Button>
      </div>
    );
  }
  if (!canRead) return null;
  if (query.isLoading) {
    return (
      <LoadingPlaceholder
        className="mt-6 border-t border-line-soft pt-5 text-sm text-ink-mid"
        role="status"
      >
        {t('loading')}
      </LoadingPlaceholder>
    );
  }
  if (query.isError) {
    return (
      <div className="mt-6 border-t border-line-soft pt-5">
        <p className="text-sm text-ink-mid">{t('loadFailed')}</p>
        <Button
          type="button"
          variant="secondary"
          size="sm"
          className="mt-3"
          onClick={() => query.refetch()}
        >
          {t('retry')}
        </Button>
      </div>
    );
  }

  const controlsDisabled = !query.isSuccess || mutation.isPending || !canUpdate;

  return (
    <form onSubmit={submit} className="mt-6 border-t border-line-soft pt-5">
      <div className="flex items-start justify-between gap-4">
        <div>
          <div className="text-sm font-medium text-ink">{t('title')}</div>
          <p className="mt-1 text-sm text-ink-mid">{t('description')}</p>
        </div>
        <Switch
          checked={enabled}
          disabled={controlsDisabled}
          onCheckedChange={(checked) => {
            setValue('enabled', checked, { shouldDirty: true });
            if (checked && !query.data?.enabled) {
              setValue('locale', uiLocale, { shouldDirty: true });
            }
          }}
          aria-label={t('title')}
        />
      </div>
      {enabled && (
        <label className="mt-4 block text-sm text-ink-mid">
          {t('language')}
          <select
            {...register('locale')}
            disabled={controlsDisabled}
            className="ml-3 rounded-md border border-line bg-paper px-3 py-2 text-ink"
          >
            <option value="ru">{t('ru')}</option>
            <option value="en">{t('en')}</option>
          </select>
        </label>
      )}
      {canUpdate && (
        <Button type="submit" size="sm" className="mt-4" disabled={controlsDisabled}>
          {mutation.isPending ? t('saving') : t('save')}
        </Button>
      )}
    </form>
  );
}
