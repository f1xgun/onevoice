// Phase 21-03 (ACCT-02) — Surfaces 5 + 6: hard-block modal triggered by
// 412 email_verification_required. Two variants:
//
//   - day0: integrations / invitations — "Подтвердите email"
//   - day7: chat / business-create after 7-day grace — "Подтвердите email,
//     чтобы продолжить"
//
// Uses AlertDialog (not Dialog) so escape-key / click-outside don't
// trivially dismiss the destructive flow. Primary action: «Отправить
// письмо снова» which POSTs /auth/verify-email/resend. Secondary action:
// «Закрыть» (day7) / «Позже» (day0) — closes the modal; the user returns
// to a read-only state.

'use client';

import { useTranslations } from 'next-intl';

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
import { toast } from '@/hooks/use-toast';

const HTTP_NO_CONTENT = 204;
const HTTP_TOO_MANY_REQUESTS = 429;

interface Props {
  open: boolean;
  onOpenChange: (v: boolean) => void;
  variant: 'day0' | 'day7';
}

export function EmailVerifiedRequiredModal({ open, onOpenChange, variant }: Props) {
  const t = useTranslations(`auth.blockedModal.${variant}`);

  async function resend() {
    try {
      const res = await fetch('/api/v1/auth/verify-email/resend', { method: 'POST' });
      if (res.status === HTTP_NO_CONTENT) {
        toast({ description: t('resendSuccess') });
      } else if (res.status === HTTP_TOO_MANY_REQUESTS) {
        toast({ description: t('throttled'), variant: 'destructive' });
      }
    } finally {
      onOpenChange(false);
    }
  }

  return (
    <AlertDialog open={open} onOpenChange={onOpenChange}>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>{t('title')}</AlertDialogTitle>
          <AlertDialogDescription>{t('body')}</AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel>{t('cancel')}</AlertDialogCancel>
          <AlertDialogAction onClick={resend}>{t('resendCta')}</AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  );
}
