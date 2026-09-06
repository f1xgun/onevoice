// Surface 10: persistent deletion-grace banner.
//
// Renders only when useAuthStore.user.accountDeletion is non-null. Red
// strip sticky to top of every (app) route. Outranks the verification
// banner per UI-SPEC Surface 10 (the deletion-grace banner mounts
// ABOVE the verification banner in (app)/layout.tsx so this fires
// first in stacking order).
//
// Inline action «Отменить удаление» fires POST /users/me/restore. On
// 204 the page reloads so /auth/me re-fetches and the banner
// unmounts. On 410 (deletion_too_old) the user is redirected to
// /login because the session is about to be invalid. Every other
// failure takes its copy from lib/error_mapping, so a transient 5xx is
// never reported as an expired cancellation window.
//
// Per UI-SPEC: ICU date interpolation via dateFnsLocale-aware
// `Intl.DateTimeFormat`; date is in font-mono per the typography
// contract.

'use client';

import { useState } from 'react';
import { useTranslations, useLocale } from 'next-intl';
import { useAuthStore } from '@/lib/auth';
import { restoreAccount, type DeletionAccountError } from '@/lib/api/account';
import { toast } from 'sonner';
import { ActionButton as Button } from '@/components/design-system/ActionButton';
import { localeToIntlTag } from '@/lib/i18n/locales';
import { mapErrorCode } from '@/lib/error_mapping';
import { cn } from '@/lib/utils';

const HTTP_GONE = 410;

export function DeletionGraceBanner() {
  const t = useTranslations('account.deletion');
  const tCopy = useTranslations();
  const locale = useLocale();
  const user = useAuthStore((s) => s.user);
  const [submitting, setSubmitting] = useState(false);

  if (!user || !user.accountDeletion) return null;

  const deletionDate = new Date(user.accountDeletion.scheduledDeletionAt);
  const dateLabel = new Intl.DateTimeFormat(localeToIntlTag(locale), {
    day: 'numeric',
    month: 'long',
  }).format(deletionDate);

  async function handleRestore() {
    setSubmitting(true);
    try {
      await restoreAccount();
      toast.success(t('restoreSuccessToast'));
      window.location.reload();
    } catch (e) {
      const err = e as DeletionAccountError;
      if (err.code === 'deletion_too_old' || err.status === HTTP_GONE) {
        toast.error(t('errors.tooOld'));
        window.location.href = '/login';
        return;
      }
      toast.error(tCopy(mapErrorCode(err.code).i18nKey));
      setSubmitting(false);
    }
  }

  return (
    <div
      role="alert"
      className={cn(
        'sticky top-0 z-40 w-full',
        'border-[var(--ov-danger)]/30 border-b',
        'bg-[var(--ov-danger-soft)] text-[var(--ov-danger)]',
        'px-4 py-3 md:px-6'
      )}
    >
      <div className="mx-auto flex max-w-7xl flex-col gap-2 md:flex-row md:items-center md:justify-between">
        <p className="text-[15px] leading-[1.55]">{t('graceBanner', { date: dateLabel })}</p>
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
