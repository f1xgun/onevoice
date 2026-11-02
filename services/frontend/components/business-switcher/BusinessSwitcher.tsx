'use client';

import Link from 'next/link';
import { useRouter } from 'next/navigation';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { useEffect, useState } from 'react';
import { Plus } from 'lucide-react';
import { useTranslations } from 'next-intl';

import { Popover, PopoverContent, PopoverTrigger } from '@/components/ui/popover';
import { ScrollArea } from '@/components/ui/scroll-area';
import { MonoLabel } from '@/components/ui/mono-label';
import { cn } from '@/lib/utils';
import { businessMark, shortBusinessAddress } from '@/lib/business-display';
import { bizApi } from '@/lib/api/business-api';
import { BIZ_API_PATHS } from '@/lib/constants/bizApiPaths';
import { QUERY_KEYS } from '@/lib/constants/queryKeys';
import type { Business } from '@/types/business';
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
  const router = useRouter();
  const queryClient = useQueryClient();
  const tSwitcher = useTranslations('team.switcher');
  const activeBusinessId = useBusinessStore((s) => s.activeBusinessId);
  const setActive = useBusinessStore((s) => s.setActive);
  const query = useBusinessList();
  const businesses = query.data ?? [];
  const isLoading = query.isLoading;
  const [open, setOpen] = useState(false);
  const [announceName, setAnnounceName] = useState<string | null>(null);
  const { containerRef, onKeyDown } = useRovingTabIndex(businesses.length);

  const { data: profile } = useQuery<Business>({
    queryKey: QUERY_KEYS.BUSINESS_PROFILE(activeBusinessId),
    queryFn: () =>
      bizApi(activeBusinessId!)
        .get<Business>(BIZ_API_PATHS.BUSINESS.ROOT)
        .then((r) => r.data),
    enabled: !!activeBusinessId,
    staleTime: 30_000,
  });
  const active = businesses.find((b) => b.id === activeBusinessId);
  const initials = active ? businessMark(active.name) : '';

  function handleSelect(b: BusinessSummary) {
    if (b.id !== activeBusinessId) {
      void queryClient.cancelQueries();
      router.replace('/chat');
      setActive(b.id);
    }
    setAnnounceName(b.name);
    setOpen(false);
  }

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
        title={triggerAria}
        disabled={isLoading}
        className={cn(
          'mb-4 flex min-h-11 w-full items-center gap-2.5 rounded-inner px-2.5 py-2 text-sm font-medium text-ink transition-colors hover:bg-paper-raised focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2'
        )}
      >
        <span
          aria-hidden
          className="flex h-9 w-9 shrink-0 items-center justify-center rounded-inner bg-brand text-brand-foreground"
        >
          {active ? initials : <Plus size={18} />}
        </span>
        <span className="min-w-0 flex-1 text-left">
          <span className="block truncate text-base font-semibold leading-5">
            {active?.name || triggerAria}
          </span>
          {profile?.address && (
            <span
              className="block truncate text-sm font-normal text-ink-soft"
              title={profile.address}
            >
              {shortBusinessAddress(profile.address)}
            </span>
          )}
        </span>
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
