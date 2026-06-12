'use client';

import { useState } from 'react';
import { useTranslations, useLocale } from 'next-intl';
import { toast } from 'sonner';

import { Button } from '@/components/ui/button';
import { localeToIntlTag } from '@/lib/i18n/locales';
import { cn } from '@/lib/utils';
import { useBusinessDeletionStore } from '@/lib/stores/businessDeletion';
import { restoreBusiness, type BusinessDeletionError } from '@/lib/api/business-deletion';

const HTTP_GONE = 410;

/**
 * Grace banner shown while a just-deleted organization is inside its 30-day
 * grace window. Mirrors the account DeletionGraceBanner. Reads the pending
 * deletion from the client-side store (soft-deleted orgs are invisible to
 * reads). «Отменить удаление» fires POST /businesses/{id}/restore; on 204 the
 * pending signal clears and the banner unmounts.
 */
export function BusinessDeletionGraceBanner() {
  const t = useTranslations('business.deletion');
  const locale = useLocale();
  const pending = useBusinessDeletionStore((s) => s.pending);
  const clear = useBusinessDeletionStore((s) => s.clear);
  const [submitting, setSubmitting] = useState(false);

  if (!pending) return null;

  const dateLabel = new Intl.DateTimeFormat(localeToIntlTag(locale), {
    day: 'numeric',
    month: 'long',
  }).format(new Date(pending.scheduledDeletionAt));

  async function handleRestore() {
    if (!pending) return;
    setSubmitting(true);
    try {
      await restoreBusiness(pending.id);
      toast.success(t('restoreSuccessToast'));
      clear();
    } catch (e) {
      const err = e as BusinessDeletionError;
      if (err.code === 'deletion_too_old' || err.status === HTTP_GONE) {
        toast.error(t('errors.tooOld'));
        clear();
        return;
      }
      toast.error(
        err.code === 'origin_not_allowed' ? t('errors.originNotAllowed') : t('errors.generic')
      );
      setSubmitting(false);
    }
  }

  return (
    <div
      role="alert"
      className={cn(
        'w-full',
        'border-[var(--ov-danger)]/30 border-b',
        'bg-[var(--ov-danger-soft)] text-[var(--ov-danger)]',
        'px-4 py-3 md:px-6'
      )}
    >
      <div className="mx-auto flex max-w-7xl flex-col gap-2 md:flex-row md:items-center md:justify-between">
        <p className="text-[15px] leading-[1.55]">
          {t('graceBanner', { name: pending.name, date: dateLabel })}
        </p>
        <Button
          variant="accent"
          size="sm"
          onClick={handleRestore}
          disabled={submitting}
          aria-label={t('graceBannerActionAria')}
        >
          {submitting ? t('graceBannerSubmitting') : t('graceBannerAction')}
        </Button>
      </div>
    </div>
  );
}
