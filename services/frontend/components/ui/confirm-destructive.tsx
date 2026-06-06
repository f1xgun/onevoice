'use client';

import { useState } from 'react';
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
import { useTranslations } from 'next-intl';
import { cn } from '@/lib/utils';

export interface ConfirmDestructiveProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  title: string;
  body: string;
  confirmLabel: string;
  pendingLabel?: string;
  cancelLabel?: string;
  onConfirm: () => Promise<void>;
}

export function ConfirmDestructive({
  open,
  onOpenChange,
  title,
  body,
  confirmLabel,
  pendingLabel,
  cancelLabel,
  onConfirm,
}: ConfirmDestructiveProps) {
  const tCommon = useTranslations('common');
  const [pending, setPending] = useState(false);

  const handleConfirm = async () => {
    setPending(true);
    try {
      await onConfirm();
    } finally {
      setPending(false);
    }
  };

  return (
    <AlertDialog open={open} onOpenChange={pending ? undefined : onOpenChange}>
      <AlertDialogContent className="max-w-md rounded-lg border border-line bg-paper-raised p-6 shadow-ov-2">
        <AlertDialogHeader className="gap-2">
          <AlertDialogTitle className="text-lg font-medium tracking-tight text-ink">
            {title}
          </AlertDialogTitle>
          <AlertDialogDescription className="text-sm leading-relaxed text-ink-mid">
            {body}
          </AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter className="mt-6 flex flex-col-reverse gap-2 sm:flex-row sm:justify-end">
          <AlertDialogCancel disabled={pending}>
            {cancelLabel ?? tCommon('cancel')}
          </AlertDialogCancel>
          <AlertDialogAction
            disabled={pending}
            onClick={(e) => {
              e.preventDefault();
              void handleConfirm();
            }}
            className={cn(
              'hover:bg-[var(--ov-danger)]/90 bg-[var(--ov-danger)] text-[var(--ov-paper-raised)]',
              pending && 'opacity-70'
            )}
          >
            {pending ? (pendingLabel ?? `${confirmLabel}…`) : confirmLabel}
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  );
}
