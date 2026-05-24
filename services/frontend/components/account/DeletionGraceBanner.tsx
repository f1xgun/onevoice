// Phase 21-04 (ACCT-03) — Surface 10: persistent deletion-grace banner.
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
// /login because the session is about to be invalid.
//
// Per UI-SPEC: ICU date interpolation via dateFnsLocale-aware
// `Intl.DateTimeFormat`; date is in font-mono per the typography
// contract.

'use client';

import { useState } from 'react';
import { useTranslations, useLocale } from 'next-intl';
import { useAuthStore } from '@/lib/auth';
import { restoreAccount, type DeletionAccountError } from '@/lib/api/account';
import { toast } from '@/hooks/use-toast';
import { Button } from '@/components/ui/button';
import { cn } from '@/lib/utils';

export function DeletionGraceBanner() {
  const t = useTranslations('account.deletion');
  const locale = useLocale();
  const user = useAuthStore((s) => s.user);
  const [submitting, setSubmitting] = useState(false);

  if (!user || !user.accountDeletion) return null;

  const deletionDate = new Date(user.accountDeletion.scheduledDeletionAt);
  const dateLabel = new Intl.DateTimeFormat(locale === 'en' ? 'en-US' : 'ru-RU', {
    day: 'numeric',
    month: 'long',
  }).format(deletionDate);

  async function handleRestore() {
    setSubmitting(true);
    try {
      await restoreAccount();
      toast({ description: t('restoreSuccessToast') });
      // Reload so /auth/me re-fetches and the banner unmounts.
      window.location.reload();
    } catch (e) {
      const err = e as DeletionAccountError;
      if (err.code === 'deletion_too_old' || err.status === 410) {
        toast({ description: t('errors.tooOld'), variant: 'destructive' });
        // Session is effectively dead — bounce to login.
        window.location.href = '/login';
        return;
      }
      toast({
        description: err.code === 'origin_not_allowed'
          ? t('errors.originNotAllowed')
          : t('errors.tooOld'),
        variant: 'destructive',
      });
      setSubmitting(false);
    }
  }

  return (
    <div
      role="alert"
      className={cn(
        'sticky top-0 z-40 w-full',
        'border-b border-[var(--ov-danger)]/30',
        'bg-[var(--ov-danger-soft)] text-[var(--ov-danger)]',
        'px-4 py-3 md:px-6'
      )}
    >
      <div className="mx-auto flex max-w-7xl flex-col gap-2 md:flex-row md:items-center md:justify-between">
        <p className="text-[15px] leading-[1.55]">
          {t('graceBanner', { date: dateLabel })}
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
