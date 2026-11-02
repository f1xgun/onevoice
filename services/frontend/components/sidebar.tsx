'use client';

import { useState } from 'react';
import { useTranslations } from 'next-intl';
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetTitle,
  SheetTrigger,
} from '@/components/ui/sheet';
import { ActionButton as Button } from '@/components/design-system/ActionButton';
import { NavRail } from '@/components/sidebar/NavRail';
import { BusinessSwitcher } from '@/components/business-switcher/BusinessSwitcher';

// The mobile menu shares destinations and account controls with the desktop shell.
export function Sidebar() {
  const tSide = useTranslations('sidebar');
  const [open, setOpen] = useState(false);

  return (
    <div className="sticky top-0 z-40 flex min-h-16 items-center gap-3 bg-background px-3 xl:hidden">
      <div className="min-w-0 flex-1 [&>button]:mb-0 [&>button]:px-0">
        <BusinessSwitcher />
      </div>
      <Sheet open={open} onOpenChange={setOpen}>
        <SheetTrigger asChild>
          <Button
            variant="ghost"
            aria-label={tSide('toggleAria')}
            className="px-0 text-brand xl:hidden"
          >
            {tSide('menuButton')}
          </Button>
        </SheetTrigger>
        <SheetContent
          data-ov-motion
          side="left"
          className="ov-app flex w-full max-w-none flex-col gap-0 overflow-y-auto overscroll-contain border-0 p-0 sm:max-w-none [&>button:first-child]:grid [&>button:first-child]:h-8 [&>button:first-child]:w-8 [&>button:first-child]:place-items-center"
        >
          <SheetTitle className="sr-only">{tSide('menuLabel')}</SheetTitle>
          <SheetDescription className="sr-only">{tSide('menuDescription')}</SheetDescription>
          <NavRail expanded onNavigate={() => setOpen(false)} />
        </SheetContent>
      </Sheet>
    </div>
  );
}
