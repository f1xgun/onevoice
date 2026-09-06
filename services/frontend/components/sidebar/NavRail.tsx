'use client';

import { useState } from 'react';
import Link from 'next/link';
import { usePathname, useRouter } from 'next/navigation';
import { useTranslations } from 'next-intl';
import {
  MessageCircle,
  Plug,
  Building2,
  Star,
  FileText,
  ListTodo,
  Settings,
  Compass,
  MessageSquarePlus,
  LogOut,
  Check,
  Minus,
} from 'lucide-react';
import { useQuery } from '@tanstack/react-query';
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from '@/components/ui/tooltip';
import { ThemeSwitcher } from '@/components/design-system/ThemeSwitcher';
import { LanguageSwitcher } from '@/components/ui/LanguageSwitcher';
import { FeedbackDialog } from '@/components/feedback/FeedbackDialog';
import { BusinessSwitcher } from '@/components/business-switcher/BusinessSwitcher';
import { cn } from '@/lib/utils';
import { bizApi } from '@/lib/api/business-api';
import { API_PATHS } from '@/lib/constants/apiPaths';
import { BIZ_API_PATHS } from '@/lib/constants/bizApiPaths';
import { QUERY_KEYS } from '@/lib/constants/queryKeys';
import { useBusinessStore } from '@/lib/stores/business';
import { usePlatformFullLabels } from '@/lib/platforms';
import { useLogout } from '@/lib/hooks/useLogout';

interface Integration {
  platform: string;
  status: string;
  last_sync_at?: string;
}

type NavItem = { href: string; labelKey: string; icon: typeof MessageCircle };
const navItems: NavItem[] = [
  { href: '/chat', labelKey: 'chat', icon: MessageCircle },
  { href: API_PATHS.INTEGRATIONS.ROOT, labelKey: 'integrations', icon: Plug },
  { href: API_PATHS.BUSINESS.ROOT, labelKey: 'business', icon: Building2 },
  { href: '/reviews', labelKey: 'reviews', icon: Star },
  { href: '/posts', labelKey: 'posts', icon: FileText },
  { href: API_PATHS.TASKS, labelKey: 'tasks', icon: ListTodo },
  { href: '/getting-started', labelKey: 'gettingStarted', icon: Compass },
  { href: '/settings', labelKey: 'settings', icon: Settings },
];

export interface NavRailProps {
  /**
   * Called whenever the user activates a nav item (logo, tab icon, logout).
   * The mobile drawer passes a setter that closes the Sheet so the user
   * isn't left staring at a half-open menu after tapping a tab.
   */
  onNavigate?: () => void;
  expanded?: boolean;
}

/** Renders navigation and labeled integration statuses with check or minus icons. */
export function NavRail({ onNavigate, expanded = false }: NavRailProps = {}) {
  const pathname = usePathname();
  const router = useRouter();
  const tNav = useTranslations('nav');
  const tSidebar = useTranslations('sidebar');
  const platformFullLabels = usePlatformFullLabels();
  const logout = useLogout();
  const activeBusinessId = useBusinessStore((s) => s.activeBusinessId);
  const [feedbackOpen, setFeedbackOpen] = useState(false);

  const { data: integrations } = useQuery<Integration[]>({
    queryKey: QUERY_KEYS.BUSINESS_INTEGRATIONS(activeBusinessId),
    queryFn: () =>
      bizApi(activeBusinessId!)
        .get(BIZ_API_PATHS.INTEGRATIONS.ROOT)
        .then((r) => (Array.isArray(r.data) ? r.data : []) as Integration[]),
    enabled: !!activeBusinessId,
    retry: false,
    placeholderData: [],
  });

  async function handleLogout() {
    await logout();
    router.push('/login');
  }

  return (
    <TooltipProvider delayDuration={150}>
      <aside
        data-testid="nav-rail"
        data-ov-motion
        aria-label={tSidebar('railWrapperAria')}
        className={cn(
          'flex shrink-0 flex-col items-center border-r border-line bg-paper-raised py-2',
          expanded ? 'h-auto w-full px-3' : 'h-dvh w-52 overflow-y-auto px-3'
        )}
      >
        <Link
          href="/chat"
          aria-label="OneVoice"
          onClick={onNavigate}
          className="mb-2 flex h-10 w-10 items-center justify-center rounded-md bg-ink text-sm font-semibold tracking-tight text-paper"
        >
          OV
        </Link>
        <BusinessSwitcher />

        <nav aria-label={tNav('railAria')} className={cn('flex flex-1 flex-col gap-1', 'w-full')}>
          {navItems.map(({ href, labelKey, icon: Icon }) => {
            const isActive = pathname === href || pathname.startsWith(`${href}/`);
            const label = tNav(labelKey);
            return (
              <Tooltip key={href}>
                <TooltipTrigger asChild>
                  <Link
                    href={href}
                    aria-label={label}
                    aria-current={isActive ? 'page' : undefined}
                    onClick={onNavigate}
                    className={cn(
                      'relative flex min-h-11 shrink-0 items-center rounded-md transition-colors',
                      'w-full gap-3 px-3 py-2',
                      isActive
                        ? 'bg-brand-soft font-semibold text-ink'
                        : 'text-ink-soft hover:bg-paper-sunken hover:text-ink'
                    )}
                  >
                    {isActive && (
                      <span
                        aria-hidden
                        className="absolute -left-2 bottom-2 top-2 w-0.5 rounded-r bg-brand"
                      />
                    )}
                    <Icon size={18} className="shrink-0" />
                    <span className="text-meta">{label}</span>
                  </Link>
                </TooltipTrigger>
                <TooltipContent side="right">{label}</TooltipContent>
              </Tooltip>
            );
          })}
        </nav>
        <Tooltip>
          <TooltipTrigger asChild>
            <div
              role="group"
              aria-label={tNav('platformsGroup')}
              className="my-2 flex flex-col gap-1.5"
              data-testid="integration-status"
            >
              {['telegram', 'vk', 'yandex_business'].map((platform) => {
                const integration = integrations?.find((i) => i.platform === platform);
                const connected = integration?.status === 'active';
                return (
                  <span key={platform} className="flex items-center gap-2 text-meta text-ink-soft">
                    {connected ? <Check size={18} aria-hidden /> : <Minus size={18} aria-hidden />}
                    <span>
                      {platformFullLabels[platform]}:{' '}
                      {tNav(connected ? 'connected' : 'notConnected')}
                    </span>
                  </span>
                );
              })}
            </div>
          </TooltipTrigger>
          <TooltipContent side="right">
            <ul className="space-y-1">
              {['telegram', 'vk', 'yandex_business'].map((platform) => {
                const integration = integrations?.find((i) => i.platform === platform);
                const connected = integration?.status === 'active';
                return (
                  <li key={platform} className="flex items-center gap-2 text-xs">
                    <span
                      className={cn(
                        'h-2 w-2 rounded-full',
                        connected ? 'bg-success' : 'bg-ink-faint'
                      )}
                    />
                    {platformFullLabels[platform]}
                  </li>
                );
              })}
            </ul>
          </TooltipContent>
        </Tooltip>
        <Tooltip>
          <TooltipTrigger asChild>
            <button
              type="button"
              onClick={() => setFeedbackOpen(true)}
              aria-label={tNav('feedback')}
              className="mb-2 flex h-10 w-10 items-center justify-center rounded-md text-ink-soft transition-colors hover:bg-paper-sunken hover:text-ink"
            >
              <MessageSquarePlus size={18} />
            </button>
          </TooltipTrigger>
          <TooltipContent side="right">{tNav('feedback')}</TooltipContent>
        </Tooltip>
        <FeedbackDialog open={feedbackOpen} onOpenChange={setFeedbackOpen} />
        <ThemeSwitcher className="mb-2" side="right" align="end" />
        <LanguageSwitcher className="mb-2" side="right" align="end" />
        <Tooltip>
          <TooltipTrigger asChild>
            <button
              type="button"
              onClick={handleLogout}
              aria-label={tNav('logout')}
              className="mb-2 flex h-10 w-10 items-center justify-center rounded-md text-ink-soft transition-colors hover:bg-paper-sunken hover:text-ink"
            >
              <LogOut size={18} />
            </button>
          </TooltipTrigger>
          <TooltipContent side="right">{tNav('logout')}</TooltipContent>
        </Tooltip>
      </aside>
    </TooltipProvider>
  );
}
