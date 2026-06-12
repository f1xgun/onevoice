// Surface E: forced re-consent modal.
//
// Renders when useAuthStore.user.requiresReconsent is non-null AND the
// user is not in the email-verification gate (modal precedence —
// EmailVerifiedRequiredModal still wins). Non-dismissible:
// - Escape suppressed via onEscapeKeyDown preventDefault
// - Backdrop click suppressed via onPointerDownOutside preventDefault
// - role="alertdialog" so screen readers treat it as a forced
// interruption requiring acknowledgement
//
// Only two exits: «Принять и продолжить» (POST /auth/consents → reload)
// or «Выйти» (logout → /login). On 409 version_mismatch the modal
// reloads so the user re-fetches the latest diff.

'use client';

import { useState } from 'react';
import { useTranslations } from 'next-intl';
import { toast } from 'sonner';
import * as DialogPrimitive from '@radix-ui/react-dialog';
import { Button } from '@/components/ui/button';
import { Checkbox } from '@/components/ui/checkbox';
import { postReconsent, type ConsentError } from '@/lib/api/consents';
import { useAuthStore, type PolicyDiff } from '@/lib/auth';
import { api } from '@/lib/api';
import { legalDocHref } from '@/lib/legal/routes';

interface ReConsentModalProps {
  policies: PolicyDiff[];
}

export function ReConsentModal({ policies }: ReConsentModalProps) {
  const t = useTranslations('reconsent.modal');
  const [checked, setChecked] = useState(false);
  const [submitting, setSubmitting] = useState(false);
  const [loggingOut, setLoggingOut] = useState(false);
  const logout = useAuthStore.getState().logout;

  async function handleAccept() {
    if (!checked || submitting) return;
    setSubmitting(true);
    try {
      await postReconsent(
        policies.map((p) => ({ slug: p.slug, version: p.newVersion, sha256: p.sha256 }))
      );
      window.location.reload();
    } catch (e) {
      const err = e as ConsentError;
      setSubmitting(false);
      if (err.code === 'version_mismatch') {
        toast.error(t('error.versionMismatch'));
        window.location.reload();
        return;
      }
      toast.error(t('error.generic'));
    }
  }

  async function handleLogout() {
    if (loggingOut) return;
    setLoggingOut(true);
    try {
      await api.post('/auth/logout').catch(() => undefined);
    } finally {
      logout();
      window.location.href = '/login';
    }
  }

  return (
    <DialogPrimitive.Root open={true}>
      <DialogPrimitive.Portal>
        <DialogPrimitive.Overlay className="fixed inset-0 z-50 bg-[oklch(0.20_0.012_60/0.55)] data-[state=open]:animate-in data-[state=open]:fade-in-0" />
        <DialogPrimitive.Content
          role="alertdialog"
          aria-modal="true"
          aria-labelledby="reconsent-heading"
          aria-describedby="reconsent-body"
          onEscapeKeyDown={(e) => e.preventDefault()}
          onPointerDownOutside={(e) => e.preventDefault()}
          onInteractOutside={(e) => e.preventDefault()}
          className="fixed left-[50%] top-[50%] z-50 grid w-full max-w-[640px] translate-x-[-50%] translate-y-[-50%] gap-4 rounded-2xl border border-[var(--ov-line)] bg-[var(--ov-paper)] p-6 shadow-2xl data-[state=open]:animate-in data-[state=open]:fade-in-0 data-[state=open]:zoom-in-95"
        >
          <div className="flex flex-col gap-2 text-left">
            <DialogPrimitive.Title
              id="reconsent-heading"
              className="text-[28px] font-medium leading-[1.2] tracking-[-0.015em] text-[var(--ov-ink)]"
            >
              {t('heading')}
            </DialogPrimitive.Title>
            <DialogPrimitive.Description
              id="reconsent-body"
              className="text-[15px] leading-[1.55] text-[var(--ov-ink-mid)]"
            >
              {t('body')}
            </DialogPrimitive.Description>
          </div>

          <div className="my-2 space-y-3">
            {policies.map((p) => (
              <div
                key={p.slug}
                className="rounded-xl border border-[var(--ov-line)] p-4 hover:bg-[var(--ov-paper-sunken)]"
              >
                <h3 className="text-[16px] font-medium text-[var(--ov-ink)]">
                  {t(`policy.${p.slug}`)}
                </h3>
                <p className="mt-1 text-[13px] text-[var(--ov-ink-mid)]">
                  {t('diff.old', { old: p.oldVersion })} · {t('diff.new', { new: p.newVersion })}
                </p>
                <a
                  href={legalDocHref(p.slug)}
                  target="_blank"
                  rel="noopener noreferrer"
                  className="mt-2 inline-block text-[14px] text-[var(--ov-accent)] hover:underline"
                >
                  {t('openDoc')} ↗
                </a>
              </div>
            ))}
          </div>

          <label className="flex cursor-pointer items-start gap-3 py-2">
            <Checkbox checked={checked} onCheckedChange={(v) => setChecked(v === true)} autoFocus />
            <span className="text-[14px] leading-[1.4] text-[var(--ov-ink)]">{t('checkbox')}</span>
          </label>

          <div className="mt-2 flex flex-col-reverse gap-2 sm:flex-row sm:justify-end sm:space-x-2">
            <Button
              type="button"
              variant="ghost"
              onClick={handleLogout}
              disabled={loggingOut || submitting}
            >
              {loggingOut ? t('cta.loggingOut') : t('cta.logout')}
            </Button>
            <Button
              type="button"
              variant="danger"
              onClick={handleAccept}
              disabled={submitting || loggingOut || !checked}
            >
              {submitting ? t('cta.accepting') : t('cta.accept')}
            </Button>
          </div>
        </DialogPrimitive.Content>
      </DialogPrimitive.Portal>
    </DialogPrimitive.Root>
  );
}
