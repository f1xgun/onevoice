'use client';

import { forwardRef, useRef } from 'react';
import type { ComponentPropsWithoutRef, ElementRef } from 'react';
import { Content } from '@radix-ui/react-dialog';
import { X } from 'lucide-react';
import { useTranslations } from 'next-intl';
import { SheetPortal, SheetOverlay, SheetClose } from '@/components/ui/sheet';
import { ActionButton } from './ActionButton';
import { cn } from '@/lib/utils';

export {
  Sheet,
  SheetTrigger,
  SheetClose,
  SheetHeader,
  SheetTitle,
  SheetDescription,
  SheetFooter,
} from '@/components/ui/sheet';

export interface AppSheetProps extends ComponentPropsWithoutRef<typeof Content> {
  side?: 'left' | 'right' | 'bottom';
}

export const SheetContent = forwardRef<ElementRef<typeof Content>, AppSheetProps>(
  function SheetContent(
    { side = 'right', className, children, onOpenAutoFocus, onCloseAutoFocus, ...props },
    ref
  ) {
    const t = useTranslations('common');
    const opener = useRef<HTMLElement | null>(null);
    return (
      <SheetPortal>
        <SheetOverlay data-ov-motion className="bg-overlay duration-150" />
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
            'fixed z-50 overflow-y-auto overscroll-contain border-control bg-card p-4 pt-16 text-ink shadow-overlay',
            side === 'bottom'
              ? 'inset-x-0 bottom-0 max-h-[calc(100dvh-2rem)] rounded-t-block pb-[max(1rem,env(safe-area-inset-bottom))] sm:mx-auto sm:max-w-[30rem]'
              : cn(
                  'inset-y-0 h-dvh w-[calc(100%-2rem)] max-w-md',
                  side === 'right' ? 'right-0 border-l' : 'left-0 border-r'
                )
          )}
        >
          <SheetClose asChild>
            <ActionButton variant="ghost" size="icon" className="absolute right-2 top-2">
              <X aria-hidden />
              <span className="sr-only">{t('close')}</span>
            </ActionButton>
          </SheetClose>
          {children}
        </Content>
      </SheetPortal>
    );
  }
);
