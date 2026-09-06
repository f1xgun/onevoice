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
import { Button } from '@/components/ui/button';
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
        <SheetContent
          side="left"
          className="flex w-80 max-w-[calc(100vw-2rem)] flex-col gap-0 overflow-y-auto overscroll-contain p-0 pt-12 [&>button:last-child]:grid [&>button:last-child]:h-8 [&>button:last-child]:w-8 [&>button:last-child]:place-items-center"
        >
          <SheetTitle className="sr-only">{tSide('menuLabel')}</SheetTitle>
          <SheetDescription className="sr-only">{tSide('menuDescription')}</SheetDescription>
          <NavRail expanded onNavigate={() => setOpen(false)} />
          {showProjectPane && (
            <div className="min-h-80 min-w-0 shrink-0">
              <ProjectPane onNavigate={() => setOpen(false)} />
            </div>
          )}
        </SheetContent>
      </Sheet>
      <span className="text-lg font-semibold">{tSide('wordmark')}</span>
    </div>
  );
}
