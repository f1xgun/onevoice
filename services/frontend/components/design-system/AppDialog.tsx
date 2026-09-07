'use client';

import { Children, Fragment, isValidElement, useRef, forwardRef } from 'react';
import type { ComponentPropsWithoutRef, ElementRef, ReactNode } from 'react';
import { Content } from '@radix-ui/react-dialog';
import { X } from 'lucide-react';
import { useTranslations } from 'next-intl';
import {
  DialogPortal,
  DialogOverlay,
  DialogClose,
  DialogHeader,
  DialogFooter,
} from '@/components/ui/dialog';
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

function dialogParts(children: ReactNode): ReturnType<typeof Children.toArray> {
  return Children.toArray(children).flatMap((child) =>
    isValidElement<{ children?: ReactNode }>(child) && child.type === Fragment
      ? dialogParts(child.props.children)
      : [child]
  );
}

export interface AppDialogProps extends ComponentPropsWithoutRef<typeof Content> {
  className?: string;
}

export const AppDialog = forwardRef<ElementRef<typeof Content>, AppDialogProps>(function AppDialog(
  { className, children, onOpenAutoFocus, onCloseAutoFocus, ...props },
  ref
) {
  const t = useTranslations('common');
  const opener = useRef<HTMLElement | null>(null);
  const parts = dialogParts(children);
  const header = parts.filter((child) => isValidElement(child) && child.type === DialogHeader);
  const footer = parts.filter((child) => isValidElement(child) && child.type === DialogFooter);
  const body = parts.filter((child) => !header.includes(child) && !footer.includes(child));
  return (
    <DialogPortal>
      <DialogOverlay data-ov-motion className="bg-overlay duration-150" />
      <Content
        {...props}
        ref={ref}
        onOpenAutoFocus={(event) => {
          opener.current =
            document.activeElement instanceof HTMLElement ? document.activeElement : null;
          onOpenAutoFocus?.(event);
        }}
        onCloseAutoFocus={(event) => {
          onCloseAutoFocus?.(event);
          if (!event.defaultPrevented && opener.current?.isConnected) {
            event.preventDefault();
            opener.current.focus();
          }
        }}
        data-ov-motion
        className={cn(
          className,
          'fixed left-1/2 top-1/2 z-50 grid max-h-[calc(100dvh-2rem)] w-[calc(100%-2rem)] max-w-[30rem] -translate-x-1/2 -translate-y-1/2 grid-rows-[auto_minmax(0,1fr)_auto] gap-4 overflow-hidden rounded-xl border border-control bg-card p-4 text-ink shadow-overlay sm:p-6'
        )}
      >
        <div className="min-w-0 pr-12">{header}</div>
        <div className="min-h-0 scroll-py-4 overflow-y-auto overscroll-contain [overflow-wrap:anywhere]">
          {body}
        </div>
        {footer.length > 0 && <div>{footer}</div>}
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
