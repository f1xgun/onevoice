'use client';

import { useState } from 'react';
import { usePathname } from 'next/navigation';
import { Menu } from 'lucide-react';
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
import { ProjectPane } from '@/components/sidebar/ProjectPane';

// Mobile-only shell. Desktop layout lives in app/(app)/layout.tsx
// (NavRail + PanelGroup with conditional ProjectPane). The mobile drawer
// auto-close-on-chat-select work is handled separately.
export function Sidebar() {
  const tSide = useTranslations('sidebar');
  const [open, setOpen] = useState(false);
  const pathname = usePathname();

  const showProjectPane = pathname.startsWith('/chat') || pathname.startsWith('/projects');

  return (
    <div className="sticky top-0 z-40 flex h-14 items-center gap-4 border-b bg-background px-4 md:hidden">
      <Sheet open={open} onOpenChange={setOpen}>
        <SheetTrigger asChild>
          <Button
            variant="ghost"
            size="icon"
            aria-label={tSide('toggleAria')}
            className="md:hidden"
          >
            <Menu className="h-5 w-5" />
          </Button>
        </SheetTrigger>
        <SheetContent side="left" className={`flex gap-0 p-0 ${showProjectPane ? 'w-72' : 'w-14'}`}>
          <SheetTitle className="sr-only">{tSide('menuLabel')}</SheetTitle>
          <SheetDescription className="sr-only">{tSide('menuDescription')}</SheetDescription>
          <NavRail onNavigate={() => setOpen(false)} />
          {showProjectPane && (
            <div className="flex-1">
              <ProjectPane onNavigate={() => setOpen(false)} />
            </div>
          )}
        </SheetContent>
      </Sheet>
      <span className="text-lg font-semibold">{tSide('wordmark')}</span>
    </div>
  );
}
