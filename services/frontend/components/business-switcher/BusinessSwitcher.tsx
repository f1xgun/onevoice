'use client';

import Link from 'next/link';
import { useEffect, useState } from 'react';
import { Plus } from 'lucide-react';
import { useTranslations } from 'next-intl';

import { Popover, PopoverContent, PopoverTrigger } from '@/components/ui/popover';
import { ScrollArea } from '@/components/ui/scroll-area';
import { MonoLabel } from '@/components/ui/mono-label';
import { cn } from '@/lib/utils';
import { useBusinessList, type BusinessSummary } from '@/lib/hooks/useBusinessList';
import { useBusinessStore } from '@/lib/stores/business';
import { useRovingTabIndex } from '@/hooks/useRovingTabIndex';

import { BusinessRow } from './BusinessRow';

const ANNOUNCE_RESET_MS = 1000;

/**
 * Popover trigger mounted at the top of NavRail. Reads `useBusinessList()`
 * for memberships and `useBusinessStore` for the persisted `activeBusinessId`.
 * Selecting a row calls `setActive(id)`; persist middleware writes
 * localStorage, and React Query keys derived from `activeBusinessId`
 * re-fetch automatically.
 *
 * Renders even with a single business so the «+ Создать организацию» CTA
 * stays available. Arrow Up/Down cycle rows via `useRovingTabIndex` (listbox
 * pattern); Tab enters the list once and falls through to the «+ Создать» link.
 */
export function BusinessSwitcher() {
  const tSwitcher = useTranslations('team.switcher');
  const activeBusinessId = useBusinessStore((s) => s.activeBusinessId);
  const setActive = useBusinessStore((s) => s.setActive);
  // `useBusinessList` ultimately returns `data ?? null` when the API responds
  // with `null` (some test harnesses use this shape for "no data yet"). The
  // destructuring default only kicks in for `undefined`, so coerce explicitly.
  const query = useBusinessList();
  const businesses = query.data ?? [];
  const isLoading = query.isLoading;
  const [open, setOpen] = useState(false);
  const [announceName, setAnnounceName] = useState<string | null>(null);
  const { containerRef, onKeyDown } = useRovingTabIndex(businesses.length);

  const active = businesses.find((b) => b.id === activeBusinessId);
  const initials = active ? active.name.slice(0, 2).toUpperCase() : '';

  function handleSelect(b: BusinessSummary) {
    setActive(b.id);
    setAnnounceName(b.name);
    setOpen(false);
  }

  // Clear the polite live region after a beat so identical re-selection
  // still triggers a fresh announcement (ME-03 in 04-REVIEW.md).
  useEffect(() => {
    if (!announceName) return;
    const t = setTimeout(() => setAnnounceName(null), ANNOUNCE_RESET_MS);
    return () => clearTimeout(t);
  }, [announceName]);

  const triggerAria = active ? tSwitcher('triggerAria', { name: active.name }) : tSwitcher('empty');

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger
        aria-label={triggerAria}
        disabled={isLoading}
        className={cn(
          'mb-2 flex h-10 w-10 items-center justify-center rounded-full text-sm font-medium tracking-tight transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2',
          active
            ? 'hover:bg-paper-sunken/80 bg-paper-sunken text-ink'
            : 'bg-paper-sunken text-ink-soft hover:text-ink'
        )}
      >
        {active ? initials : <Plus size={18} aria-hidden />}
      </PopoverTrigger>
      <PopoverContent
        side="right"
        align="start"
        sideOffset={12}
        className="min-w-[260px] max-w-[320px] rounded-lg border border-line bg-paper-raised p-1 shadow-ov-2"
        role="menu"
      >
        <span aria-live="polite" className="sr-only">
          {announceName ? tSwitcher('selectedAnnounce', { name: announceName }) : ''}
        </span>

        {businesses.length > 0 && (
          <div className="px-3 pb-1 pt-2">
            <MonoLabel>{tSwitcher('kicker')}</MonoLabel>
          </div>
        )}

        {businesses.length > 0 && (
          <ScrollArea className="max-h-[288px]">
            <div
              ref={containerRef as React.RefObject<HTMLDivElement>}
              onKeyDown={onKeyDown}
              className="flex flex-col gap-0.5"
            >
              {businesses.map((b) => (
                <BusinessRow
                  key={b.id}
                  business={b}
                  isActive={b.id === activeBusinessId}
                  onSelect={handleSelect}
                />
              ))}
            </div>
          </ScrollArea>
        )}

        <div className="my-1 border-t border-line-soft" aria-hidden />

        <Link
          href="/business/new"
          onClick={() => setOpen(false)}
          className="flex w-full items-center gap-2 rounded-md px-3 py-3 text-sm text-ink-mid transition-colors hover:bg-paper-sunken hover:text-ink"
        >
          <Plus size={16} aria-hidden />
          {tSwitcher('create')}
        </Link>
      </PopoverContent>
    </Popover>
  );
}
