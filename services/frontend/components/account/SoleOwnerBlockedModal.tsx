// Surface 9: sole-owner-of-businesses 409
// conflict dialog. Triggered when DELETE /users/me returns
// `409 {code: "sole_owner_of_businesses", businesses: [...]}`.
//
// Per UI-SPEC checker flag D1 + plan v1.4 reality check: ownership
// transfer is deferred to v1.5. For v1.4 we render the «Передать
// права» button DISABLED with a tooltip explaining "Available in the
// next version — delete the business or wait". The «Удалить бизнес»
// path links to the existing /business/{id} settings page.

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
} from '@/components/design-system/AppAlertDialog';
import { Badge } from '@/components/ui/badge';
import { ActionButton as Button } from '@/components/design-system/ActionButton';
import { cn } from '@/lib/utils';
import type { SoleOwnerBusiness } from '@/lib/api/account';

interface SoleOwnerBlockedModalProps {
  open: boolean;
  businesses: SoleOwnerBusiness[];
  onOpenChange: (open: boolean) => void;
}

const MAX_VISIBLE = 5;

export function SoleOwnerBlockedModal({
  open,
  businesses,
  onOpenChange,
}: SoleOwnerBlockedModalProps) {
  const t = useTranslations('account.deletion.soleOwnerModal');

  const visible = businesses.slice(0, MAX_VISIBLE);
  const overflow = businesses.length - visible.length;

  return (
    <AlertDialog open={open} onOpenChange={onOpenChange}>
      <AlertDialogContent className="max-w-[480px]">
        <AlertDialogHeader>
          <AlertDialogTitle className="text-[28px] font-medium leading-[1.2] tracking-[-0.015em]">
            {t('heading')}
          </AlertDialogTitle>
          <AlertDialogDescription className="text-[15px] leading-[1.55] text-[var(--ov-ink-mid)]">
            {t('body')}
          </AlertDialogDescription>
        </AlertDialogHeader>

        <ul className="my-4 space-y-2">
          {visible.map((b) => (
            <li
              key={b.id}
              className={cn(
                'flex items-center justify-between gap-3 rounded-md border border-[var(--ov-line)]',
                'bg-[var(--ov-paper-raised)] p-3'
              )}
            >
              <div className="flex flex-1 items-center gap-2">
                <span className="text-[14px] font-medium">{b.name}</span>
                <Badge className="bg-warning-soft text-[var(--ov-warning-ink)]">
                  {t('requiresAction')}
                </Badge>
              </div>
              <div className="flex gap-2">
                <Button variant="primary" size="sm" disabled title={t('transferDisabledTooltip')}>
                  {t('transferButton')}
                </Button>
                <Button variant="danger" size="sm" asChild>
                  <a href={`/business/${b.id}`}>{t('deleteBusinessButton')}</a>
                </Button>
              </div>
            </li>
          ))}
          {overflow > 0 && (
            <li className="px-3 text-[13px] text-[var(--ov-ink-mid)]">
              {t('andMore', { count: overflow })}
            </li>
          )}
        </ul>

        <AlertDialogFooter>
          <AlertDialogCancel onClick={() => onOpenChange(false)}>{t('cancel')}</AlertDialogCancel>
          {/* Defensive: AlertDialogAction so escape/keyboard exits work the same
              as Cancel — no destructive default-focus path. */}
          <AlertDialogAction asChild>
            <Button variant="ghost" onClick={() => onOpenChange(false)}>
              {t('cancel')}
            </Button>
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  );
}
