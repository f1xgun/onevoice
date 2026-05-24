// Phase 21-03 (ACCT-02) — Surface 4: Persistent verification banner.
//
// Renders only when useAuthStore.user.emailVerified === false. Sticks to
// the top of every (app) route until the user verifies. Two inline
// actions:
//   1. «Отправить письмо снова» → POST /auth/verify-email/resend
//      with a 60s cooldown on success/throttle (matches the backend's
//      1/min rate limit per D-24).
//   2. «Я ошибся в email» → opens the EmailChangeBeforeVerifyModal
//      (D-21 escape hatch for dead email-on-file).
//
// The banner cohabits with StickyAlert's `sticky top-0` semantics.
// Future deletion-grace banner (Phase 21-04) will outrank it; for now
// this is the only sticky alert in (app).

'use client';

import { useState, useEffect } from 'react';
import { useTranslations } from 'next-intl';
import { useAuthStore } from '@/lib/auth';
import { Button } from '@/components/ui/button';
import { toast } from '@/hooks/use-toast';
import { EmailChangeBeforeVerifyModal } from './EmailChangeBeforeVerifyModal';

const HTTP_NO_CONTENT = 204;
const HTTP_TOO_MANY_REQUESTS = 429;
const RESEND_COOLDOWN_SECONDS = 60;

export function VerificationBanner() {
  const t = useTranslations('auth.banner');
  const user = useAuthStore((s) => s.user);
  const [cooldownSec, setCooldownSec] = useState(0);
  const [editOpen, setEditOpen] = useState(false);

  // Tick the cooldown countdown. Clears itself on unmount.
  useEffect(() => {
    if (cooldownSec <= 0) return;
    const id = setTimeout(() => setCooldownSec((s) => s - 1), 1000);
    return () => clearTimeout(id);
  }, [cooldownSec]);

  // Render nothing for verified users / no-session pages — the banner is
  // explicitly opt-in via me.emailVerified===false.
  if (!user || user.emailVerified !== false) return null;

  const deadline = user.emailVerificationDeadline ? new Date(user.emailVerificationDeadline) : null;
  const deadlineLabel = deadline
    ? deadline.toLocaleDateString('ru-RU', { day: 'numeric', month: 'long' })
    : '';

  async function resend() {
    try {
      const res = await fetch('/api/v1/auth/verify-email/resend', { method: 'POST' });
      if (res.status === HTTP_NO_CONTENT) {
        toast({ description: t('resendSuccess') });
        setCooldownSec(RESEND_COOLDOWN_SECONDS);
      } else if (res.status === HTTP_TOO_MANY_REQUESTS) {
        toast({ description: t('throttled'), variant: 'destructive' });
        setCooldownSec(RESEND_COOLDOWN_SECONDS);
      } else {
        toast({ description: t('genericError'), variant: 'destructive' });
      }
    } catch {
      toast({ description: t('genericError'), variant: 'destructive' });
    }
  }

  return (
    <>
      <div
        role="alert"
        className="sticky top-0 z-10 flex flex-wrap items-center gap-3 border-b bg-warning-soft px-6 py-3 text-[15px] leading-[1.55] text-[var(--ov-warning-ink)]"
      >
        <span aria-hidden="true" className="h-2 w-2 shrink-0 rounded-full bg-[var(--ov-warning)]" />
        <span className="min-w-0 flex-1">
          {deadlineLabel ? t('body', { date: deadlineLabel }) : t('bodyNoDate')}
        </span>
        <Button
          variant="link"
          size="sm"
          disabled={cooldownSec > 0}
          onClick={resend}
          aria-live="polite"
        >
          {cooldownSec > 0 ? t('resendCooldown', { sec: cooldownSec }) : t('resendCta')}
        </Button>
        <Button variant="link" size="sm" onClick={() => setEditOpen(true)}>
          {t('changeEmailCta')}
        </Button>
      </div>
      <EmailChangeBeforeVerifyModal open={editOpen} onOpenChange={setEditOpen} />
    </>
  );
}
