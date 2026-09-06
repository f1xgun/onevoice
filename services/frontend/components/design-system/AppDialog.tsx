'use client';

import { forwardRef } from 'react';
import type { ComponentPropsWithoutRef, ElementRef } from 'react';
import { Content } from '@radix-ui/react-dialog';
import { X } from 'lucide-react';
import { useTranslations } from 'next-intl';
import { DialogPortal, DialogOverlay, DialogClose } from '@/components/ui/dialog';
import { cn } from '@/lib/utils';
import { ActionButton } from './ActionButton';

export {
  Dialog,
  DialogTrigger,
  DialogClose,
  DialogTitle,
  DialogDescription,
  DialogHeader,
  DialogFooter,
} from '@/components/ui/dialog';

export interface AppDialogProps extends ComponentPropsWithoutRef<typeof Content> {
  className?: string;
}

export const AppDialog = forwardRef<ElementRef<typeof Content>, AppDialogProps>(function AppDialog(
  { className, children, ...props },
  ref
) {
  const t = useTranslations('common');
  return (
    <DialogPortal>
      <DialogOverlay data-ov-motion className="bg-overlay duration-150" />
      <Content
        {...props}
        ref={ref}
        data-ov-motion
        className={cn(
          className,
          'fixed left-1/2 top-1/2 z-50 grid max-h-[calc(100dvh-2rem)] w-[calc(100%-2rem)] max-w-[30rem] -translate-x-1/2 -translate-y-1/2 grid-rows-[auto_minmax(0,1fr)_auto] gap-4 overflow-y-auto rounded-xl border border-control bg-card p-4 text-ink shadow-overlay sm:p-6 [&>:first-child]:pr-12 [&>div:nth-child(2)]:min-h-0 [&>div:nth-child(2)]:overflow-y-auto'
        )}
      >
        {children}
        <DialogClose asChild>
          <ActionButton variant="ghost" size="icon" className="absolute right-2 top-2">
            <X aria-hidden="true" />
            <span className="sr-only">{t('close')}</span>
          </ActionButton>
        </DialogClose>
      </Content>
    </DialogPortal>
  );
});
