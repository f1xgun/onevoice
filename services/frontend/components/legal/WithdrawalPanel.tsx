// Surface F: PDN withdrawal panel inside /settings/privacy.
//
// Fetches the user's three consent rows via listMyConsents, renders
// one card per slug. The PDN row carries a destructive «Отозвать
// согласие на ПДн» button that opens an AlertDialog confirming
// destruction. On confirm → withdrawPDN → server triggers the
// RequestDeletion flow (30-day grace). 423 is treated as
// success since the deletion is already pending (UI-SPEC §F edge case).
//
// If user.accountDeletion is already non-null, the button is hidden in
// favour of an «alreadyScheduled» notice linking back to /settings/account
// where cancel-deletion lives (UI-SPEC §F edge case).

'use client';

import { useEffect, useState } from 'react';
import { useTranslations, useLocale, useFormatter } from 'next-intl';
import { toast } from 'sonner';
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from '@/components/ui/alert-dialog';
import { Button } from '@/components/ui/button';
import {
  listMyConsents,
  withdrawPDN,
  type ConsentError,
  type ConsentRecord,
} from '@/lib/api/consents';
import { useAuthStore } from '@/lib/auth';
import { HTTP_STATUS } from '@/lib/constants/httpStatus';
import type { PolicySlug } from '@/lib/legal/versions';

const ORDERED_SLUGS: readonly PolicySlug[] = ['tos', 'privacy', 'pdn'] as const;

export function WithdrawalPanel() {
  const t = useTranslations('settings.privacy');
  const tDialog = useTranslations('settings.privacy.dialog');
  const tToast = useTranslations('settings.privacy.toast');
  const locale = useLocale();
  const format = useFormatter();
  const user = useAuthStore((s) => s.user);

  const [consents, setConsents] = useState<ConsentRecord[] | null>(null);
  const [loadError, setLoadError] = useState(false);
  const [dialogOpen, setDialogOpen] = useState(false);
  const [submitting, setSubmitting] = useState(false);

  useEffect(() => {
    let cancelled = false;
    listMyConsents()
      .then((rows) => {
        if (!cancelled) setConsents(rows);
      })
      .catch(() => {
        if (!cancelled) setLoadError(true);
      });
    return () => {
      cancelled = true;
    };
  }, []);

  function formatDate(iso: string): string {
    if (!iso) return '—';
    const parsed = new Date(iso);
    if (Number.isNaN(parsed.getTime())) return iso;
    return format.dateTime(parsed, { day: 'numeric', month: 'long', year: 'numeric' });
  }

  async function handleConfirm() {
    if (submitting) return;
    setSubmitting(true);
    try {
      await withdrawPDN();
      toast.success(tToast('scheduled', { deletionDate: '—' }));
      setDialogOpen(false);
      window.location.reload();
    } catch (e) {
      const err = e as ConsentError;
      if (err.status === HTTP_STATUS.LOCKED) {
        toast.success(tToast('scheduled', { deletionDate: err.deletionDate ?? '—' }));
        setDialogOpen(false);
        window.location.reload();
        return;
      }
      setSubmitting(false);
      toast.error(tToast('error'));
    }
  }

  const alreadyScheduled = user?.accountDeletion != null;
  const alreadyScheduledDate = alreadyScheduled
    ? formatDate(user?.accountDeletion?.scheduledDeletionAt ?? '')
    : '';

  if (loadError) {
    return (
      <p role="alert" className="text-[14px] text-[var(--ov-danger)]">
        {t('loadError')}
      </p>
    );
  }

  if (!consents) {
    return (
      <p aria-live="polite" className="text-[14px] text-[var(--ov-ink-mid)]">
        {t('loading')}
      </p>
    );
  }

  const bySlug = new Map(consents.map((c) => [c.slug, c]));

  return (
    <section aria-labelledby="withdrawal-heading" className="space-y-6">
      <h2
        id="withdrawal-heading"
        className="text-[24px] font-medium leading-[1.2] tracking-[-0.015em] text-[var(--ov-ink)]"
      >
        {t('title')}
      </h2>
      <p className="text-[15px] leading-[1.55] text-[var(--ov-ink-mid)]">{t('subtitle')}</p>

      <div className="space-y-4">
        {ORDERED_SLUGS.map((slug) => {
          const record = bySlug.get(slug);
          return (
            <article
              key={slug}
              className="rounded-xl border border-[var(--ov-line)] bg-[var(--ov-paper-raised)] p-6"
            >
              <h3 className="text-[16px] font-medium text-[var(--ov-ink)]">{t(`row.${slug}`)}</h3>
              <p className="mt-1 font-mono text-[13px] text-[var(--ov-ink-mid)]">
                {record
                  ? t('acceptedAt', {
                      acceptedAt: formatDate(record.acceptedAt),
                      version: record.version,
                    })
                  : '—'}
              </p>
              <a
                href={`/legal/${slug}`}
                target="_blank"
                rel="noopener noreferrer"
                className="mt-2 inline-block text-[14px] text-[var(--ov-accent)] hover:underline"
              >
                {t('openDoc')} ↗
              </a>

              {slug === 'pdn' && (
                <div className="mt-4 space-y-3">
                  <p className="rounded-md border border-[var(--ov-line)] bg-[var(--ov-paper-sunken)] px-4 py-3 text-[13px] leading-[1.55] text-[var(--ov-ink-mid)]">
                    {t('pdn.note')}
                  </p>
                  {alreadyScheduled ? (
                    <p className="text-[13px] text-[var(--ov-danger)]">
                      {t('pdn.alreadyScheduled', { deletionDate: alreadyScheduledDate })}
                    </p>
                  ) : (
                    <Button
                      variant="danger"
                      size="sm"
                      onClick={() => setDialogOpen(true)}
                      aria-haspopup="dialog"
                      lang={locale}
                    >
                      {t('pdn.cta')}
                    </Button>
                  )}
                </div>
              )}
            </article>
          );
        })}
      </div>

      <AlertDialog open={dialogOpen} onOpenChange={(o) => !submitting && setDialogOpen(o)}>
        <AlertDialogContent className="max-w-[480px]">
          <AlertDialogHeader>
            <AlertDialogTitle className="text-[24px] font-medium leading-[1.2] tracking-[-0.015em] text-[var(--ov-ink)]">
              {tDialog('title')}
            </AlertDialogTitle>
            <AlertDialogDescription
              id="withdrawal-dialog-body"
              className="text-[15px] leading-[1.55] text-[var(--ov-ink-mid)]"
            >
              {tDialog('body')}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={submitting}>{tDialog('cancel')}</AlertDialogCancel>
            <AlertDialogAction
              asChild
              onClick={(e) => {
                e.preventDefault();
                void handleConfirm();
              }}
            >
              <Button
                variant="danger"
                disabled={submitting}
                aria-describedby="withdrawal-dialog-body"
              >
                {submitting ? t('pdn.submitting') : tDialog('confirm')}
              </Button>
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </section>
  );
}
