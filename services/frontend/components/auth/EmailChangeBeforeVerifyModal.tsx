// Phase 21-03 (ACCT-02) — Surface 4 secondary: escape hatch for users
// with a dead pre-verification email (D-21). Renders a Dialog (NOT
// AlertDialog — non-destructive edit) with a single email field.
//
// On success the modal closes + a toast confirms. The banner stays
// visible (the new email is still unverified) but the next /auth/me
// refresh updates the deadline.

'use client';

import { useState } from 'react';
import { useTranslations } from 'next-intl';

import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { toast } from '@/hooks/use-toast';

const HTTP_NO_CONTENT = 204;
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
      const res = await fetch('/api/v1/auth/email-before-verify', {
        method: 'PATCH',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ newEmail }),
      });
      if (res.status === HTTP_NO_CONTENT) {
        toast({ description: t('changeSuccess', { newEmail }) });
        onOpenChange(false);
        setNewEmail('');
        // Soft refresh — the layout's next /auth/me roundtrip will pick
        // up the new email + reset the deadline. Force a reload so
        // the banner re-renders against the updated user object.
        if (typeof window !== 'undefined') window.location.reload();
        return;
      }
      if (res.status === HTTP_CONFLICT) {
        setError(t('emailTaken'));
        return;
      }
      if (res.status === HTTP_FORBIDDEN) {
        setError(t('alreadyVerified'));
        return;
      }
      setError(t('genericError'));
    } catch {
      setError(t('genericError'));
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
