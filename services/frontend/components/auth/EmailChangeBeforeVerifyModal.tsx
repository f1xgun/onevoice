// Surface 4 secondary: escape hatch for users
// with a dead pre-verification email. Renders a Dialog (NOT
// AlertDialog — non-destructive edit) with a single email field.
//
// On success the modal closes + a toast confirms. The banner stays
// visible (the new email is still unverified) but the next /auth/me
// refresh updates the deadline.

'use client';

import { useState } from 'react';
import { useTranslations } from 'next-intl';

import { api } from '@/lib/api';
import { API_PATHS } from '@/lib/constants/apiPaths';
import {
  Dialog,
  AppDialog as DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/design-system/AppDialog';
import { ActionButton as Button } from '@/components/design-system/ActionButton';
import { AppInput as Input } from '@/components/design-system/AppInput';
import { Label } from '@/components/ui/label';
import { toast } from 'sonner';

const HTTP_CONFLICT = 409;
const HTTP_FORBIDDEN = 403;

interface Props {
  open: boolean;
  onOpenChange: (v: boolean) => void;
}

export function EmailChangeBeforeVerifyModal({ open, onOpenChange }: Props) {
  const t = useTranslations('auth.banner');
  const [newEmail, setNewEmail] = useState('');
  const [error, setError] = useState<string | null>(null);
  const [pending, setPending] = useState(false);

  async function submit() {
    if (!newEmail) return;
    setPending(true);
    setError(null);
    try {
      await api.patch(API_PATHS.AUTH.EMAIL_BEFORE_VERIFY, { newEmail });
      toast.success(t('changeSuccess', { newEmail }));
      onOpenChange(false);
      setNewEmail('');
      if (typeof window !== 'undefined') window.location.reload();
    } catch (err) {
      const status = (err as { response?: { status?: number } })?.response?.status;
      if (status === HTTP_CONFLICT) {
        setError(t('emailTaken'));
      } else if (status === HTTP_FORBIDDEN) {
        setError(t('alreadyVerified'));
      } else {
        setError(t('genericError'));
      }
    } finally {
      setPending(false);
    }
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{t('changeTitle')}</DialogTitle>
          <DialogDescription>{t('changeBody')}</DialogDescription>
        </DialogHeader>
        <div className="flex flex-col gap-2">
          <Label htmlFor="new-email-before-verify">{t('newEmailLabel')}</Label>
          <Input
            id="new-email-before-verify"
            type="email"
            value={newEmail}
            onChange={(e) => setNewEmail(e.target.value)}
            disabled={pending}
            autoComplete="email"
          />
          {error ? (
            <p className="mt-1 text-sm text-danger" aria-live="polite">
              {error}
            </p>
          ) : null}
        </div>
        <DialogFooter>
          <Button variant="ghost" onClick={() => onOpenChange(false)} disabled={pending}>
            {t('cancel')}
          </Button>
          <Button onClick={submit} disabled={pending || !newEmail}>
            {t('changeCta')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
