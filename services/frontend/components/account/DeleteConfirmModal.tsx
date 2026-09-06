// Surface 8: account-deletion confirmation modal.
//
// AlertDialog with verbatim RU copy from UI-SPEC. Composes:
// - Password re-entry (per OneVoice security model; not the
// "type УДАЛИТЬ" pattern)
// - Submit POSTs DELETE /api/v1/users/me
// - On 204: redirects to /login + success toast
// - On 401 password_invalid: inline field error «Неверный пароль»
// - On 409 sole_owner_of_businesses: closes self + opens
// SoleOwnerBlockedModal with the returned businesses payload
// - On 423 account_pending_deletion: closes self + reloads (the
// grace banner will mount on the next render)
// - On any other code (origin_not_allowed, 5xx, network): copy comes from
// lib/error_mapping, so an unrelated failure is never reported as a wrong
// password

'use client';

import { useState } from 'react';
import { useTranslations, useLocale } from 'next-intl';
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
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { FieldError } from '@/components/ui/field-error';
import { Button } from '@/components/ui/button';
import { toast } from 'sonner';
import {
  deleteAccount,
  type DeletionAccountError,
  type SoleOwnerBusiness,
} from '@/lib/api/account';
import { localeToIntlTag } from '@/lib/i18n/locales';
import { mapErrorCode } from '@/lib/error_mapping';
import { SoleOwnerBlockedModal } from './SoleOwnerBlockedModal';

interface DeleteConfirmModalProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
}

const MIN_PASSWORD_LEN = 8;
const DELETION_GRACE_DAYS = 30;
const HOURS_PER_DAY = 24;
const MINUTES_PER_HOUR = 60;
const SECONDS_PER_MINUTE = 60;
const MS_PER_SECOND = 1000;
const DELETION_GRACE_MS =
  DELETION_GRACE_DAYS * HOURS_PER_DAY * MINUTES_PER_HOUR * SECONDS_PER_MINUTE * MS_PER_SECOND;

export function DeleteConfirmModal({ open, onOpenChange }: DeleteConfirmModalProps) {
  const t = useTranslations('account.deletion.confirmModal');
  const tErrors = useTranslations('account.deletion.errors');
  const tCopy = useTranslations(); // top-level for COPY i18nKey resolution
  const locale = useLocale();

  const [password, setPassword] = useState('');
  const [submitting, setSubmitting] = useState(false);
  const [fieldError, setFieldError] = useState<string | null>(null);
  const [soleOwnerOpen, setSoleOwnerOpen] = useState(false);
  const [soleOwnerBusinesses, setSoleOwnerBusinesses] = useState<SoleOwnerBusiness[]>([]);

  const deletionDate = new Date(Date.now() + DELETION_GRACE_MS);
  const dateLabel = new Intl.DateTimeFormat(localeToIntlTag(locale), {
    day: 'numeric',
    month: 'long',
    year: 'numeric',
  }).format(deletionDate);

  function reset() {
    setPassword('');
    setSubmitting(false);
    setFieldError(null);
  }

  async function handleSubmit() {
    if (password.length < MIN_PASSWORD_LEN) {
      setFieldError(t('passwordTooShort'));
      return;
    }
    setFieldError(null);
    setSubmitting(true);
    try {
      await deleteAccount(password);
      toast.success(t('successToast'));
      window.location.href = '/login';
    } catch (e) {
      const err = e as DeletionAccountError;
      setSubmitting(false);
      switch (err.code) {
        case 'password_invalid':
          setFieldError(tErrors('passwordInvalid'));
          return;
        case 'sole_owner_of_businesses':
          setSoleOwnerBusinesses(err.businesses ?? []);
          setSoleOwnerOpen(true);
          onOpenChange(false);
          return;
        case 'account_pending_deletion':
          toast.error(tErrors('pendingDeletion'));
          onOpenChange(false);
          window.location.reload();
          return;
        default:
          toast.error(tCopy(mapErrorCode(err.code).i18nKey));
      }
    }
  }

  return (
    <>
      <AlertDialog
        open={open}
        onOpenChange={(nextOpen) => {
          if (!nextOpen) reset();
          onOpenChange(nextOpen);
        }}
      >
        <AlertDialogContent className="max-w-[480px]">
          <AlertDialogHeader>
            <AlertDialogTitle className="text-[28px] font-medium leading-[1.2] tracking-[-0.015em]">
              {t('heading')}
            </AlertDialogTitle>
            <AlertDialogDescription className="text-[15px] leading-[1.55] text-[var(--ov-ink-mid)]">
              {t('body', { date: dateLabel })}
            </AlertDialogDescription>
          </AlertDialogHeader>

          <div className="my-4 space-y-2">
            <Label htmlFor="delete-confirm-password" className="text-sm font-medium">
              {t('passwordLabel')}
            </Label>
            <Input
              id="delete-confirm-password"
              type="password"
              autoComplete="current-password"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              placeholder={t('passwordPlaceholder')}
              disabled={submitting}
            />
            {fieldError ? <FieldError>{fieldError}</FieldError> : null}
          </div>

          <AlertDialogFooter>
            <AlertDialogCancel disabled={submitting}>{t('cancel')}</AlertDialogCancel>
            <AlertDialogAction
              asChild
              onClick={(e) => {
                e.preventDefault();
                void handleSubmit();
              }}
            >
              <Button variant="danger" disabled={submitting || password.length < MIN_PASSWORD_LEN}>
                {submitting ? t('submitting') : t('cta')}
              </Button>
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>

      <SoleOwnerBlockedModal
        open={soleOwnerOpen}
        businesses={soleOwnerBusinesses}
        onOpenChange={setSoleOwnerOpen}
      />
    </>
  );
}
