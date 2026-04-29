'use client';

import { useState } from 'react';
import { usePathname } from 'next/navigation';
import { Menu } from 'lucide-react';
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

// Mobile-only shell. Desktop layout lives in app/(app)/layout.tsx (see
// 19-01 D-14: NavRail + PanelGroup with conditional ProjectPane). Phase
// 19-05 will own the mobile drawer auto-close-on-chat-select work.
export function Sidebar() {
  const [open, setOpen] = useState(false);
  const pathname = usePathname();

  // Same route-gating contract as desktop: ProjectPane only renders on
  // /chat/* and /projects/*. Other routes show only the NavRail in the
  // drawer.
  const showProjectPane = pathname.startsWith('/chat') || pathname.startsWith('/projects');

  return (
    <div className="sticky top-0 z-40 flex h-14 items-center gap-4 border-b bg-background px-4 md:hidden">
      <Sheet open={open} onOpenChange={setOpen}>
        <SheetTrigger asChild>
          <Button
            variant="ghost"
            size="icon"
            aria-label="Открыть боковое меню"
            className="md:hidden"
          >
            <Menu className="h-5 w-5" />
          </Button>
        </SheetTrigger>
        <SheetContent
          side="left"
          // Drawer width follows content: full 288 px when the project tree
          // is shown (chat / projects routes), tight 56 px (just the
          // NavRail) on every other route — no awkward empty cream gap.
          className={`flex gap-0 p-0 ${showProjectPane ? 'w-72' : 'w-14'}`}
        >
          <SheetTitle className="sr-only">Боковое меню</SheetTitle>
          <SheetDescription className="sr-only">
            Навигация по приложению и список проектов
          </SheetDescription>
          <NavRail onNavigate={() => setOpen(false)} />
          {showProjectPane && (
            <div className="flex-1">
              <ProjectPane onNavigate={() => setOpen(false)} />
            </div>
          )}
        </SheetContent>
      </Sheet>
      <span className="text-lg font-semibold">OneVoice</span>
    </div>
  );
}
