'use client';

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
  LogOut,
} from 'lucide-react';
import { useQuery } from '@tanstack/react-query';
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from '@/components/ui/tooltip';
import { LanguageSwitcher } from '@/components/ui/LanguageSwitcher';
import { BusinessSwitcher } from '@/components/business-switcher/BusinessSwitcher';
import { cn } from '@/lib/utils';
import { useAuthStore } from '@/lib/auth';
import { bizApi } from '@/lib/api/business-api';
import { API_PATHS } from '@/lib/constants/apiPaths';
import { BIZ_API_PATHS } from '@/lib/constants/bizApiPaths';
import { QUERY_KEYS } from '@/lib/constants/queryKeys';
import { useBusinessStore } from '@/lib/stores/business';
import { PLATFORM_FULL_LABELS } from '@/lib/platforms';
import { queryClient } from '@/lib/queryClient';
import { BUSINESS_LIST_QUERY_KEY } from '@/lib/hooks/useBusinessList';

interface Integration {
  platform: string;
  status: string;
  last_sync_at?: string;
}

// Permanent icon-only nav-rail: width 56–64 px, vertical icon column,
// rendered on every authenticated route. Project tree, search, and pinned
// rows live in <ProjectPane> — NOT here.
//
// Order locked by design handoff README v2 §5: Чат → Интеграции → Профиль
// бизнеса → Отзывы → Посты → Задачи → Настройки. labelKey resolves through
// nav.* in messages/ru.json so the rendered tooltip + aria-label localize
// in lockstep.
type NavItem = { href: string; labelKey: string; icon: typeof MessageCircle };
const navItems: NavItem[] = [
  { href: '/chat', labelKey: 'chat', icon: MessageCircle },
  { href: API_PATHS.INTEGRATIONS.ROOT, labelKey: 'integrations', icon: Plug },
  { href: API_PATHS.BUSINESS.ROOT, labelKey: 'business', icon: Building2 },
  { href: '/reviews', labelKey: 'reviews', icon: Star },
  { href: '/posts', labelKey: 'posts', icon: FileText },
  { href: API_PATHS.TASKS, labelKey: 'tasks', icon: ListTodo },
  { href: '/settings', labelKey: 'settings', icon: Settings },
];

export interface NavRailProps {
  /**
   * Called whenever the user activates a nav item (logo, tab icon, logout).
   * The mobile drawer passes a setter that closes the Sheet so the user
   * isn't left staring at a half-open menu after tapping a tab.
   */
  onNavigate?: () => void;
}

export function NavRail({ onNavigate }: NavRailProps = {}) {
  const pathname = usePathname();
  const router = useRouter();
  const tNav = useTranslations('nav');
  const logout = useAuthStore((s) => s.logout);
  const activeBusinessId = useBusinessStore((s) => s.activeBusinessId);

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

  function handleLogout() {
    // Phase 5: drop session-scoped React-Query caches BEFORE the auth store
    // clears so any in-flight subscription gets the empty state.
    //   - ['businesses', ...] sweeps members, invitations, roles, permissions
    //     by partial-match (all are nested under that prefix).
    //   - ['permissions-catalog'] is top-level and persists across login by
    //     default (app-static); we remove it here so a different actor's
    //     deploy build can re-fetch fresh.
    queryClient.removeQueries({ queryKey: BUSINESS_LIST_QUERY_KEY });
    queryClient.removeQueries({ queryKey: QUERY_KEYS.PERMISSIONS_CATALOG });
    logout();
    router.push('/login');
  }

  return (
    <TooltipProvider delayDuration={150}>
      <aside
        data-testid="nav-rail"
        className="flex h-screen w-14 shrink-0 flex-col items-center border-r border-line bg-paper-raised py-2"
      >
        {/* BusinessSwitcher — visible payoff of the v2.0 multi-tenant model.
            40x40 circular trigger above the OV mark; opens a Popover with the
            user's memberships and a «+ Создать организацию» footer. See
            components/business-switcher/BusinessSwitcher.tsx and UI-SPEC §S-1. */}
        <BusinessSwitcher />

        {/* OV mark — graphite on paper, the one always-visible brand cue. */}
        <Link
          href="/chat"
          aria-label="OneVoice"
          onClick={onNavigate}
          className="mb-4 flex h-10 w-10 items-center justify-center rounded-md bg-ink text-sm font-semibold tracking-tight text-paper"
        >
          OV
        </Link>

        {/* Vertical nav-list. Active state: ink icon + 2px ochre left bar
            (no background change). Idle: ink-soft → ink on hover with
            paper-sunken wash. */}
        <nav className="flex flex-1 flex-col gap-1">
          {navItems.map(({ href, labelKey, icon: Icon }) => {
            const isActive = pathname.startsWith(href);
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
                      'relative flex h-10 w-10 items-center justify-center rounded-md transition-colors',
                      isActive ? 'text-ink' : 'text-ink-soft hover:bg-paper-sunken hover:text-ink'
                    )}
                  >
                    {isActive && (
                      <span
                        aria-hidden
                        className="absolute -left-2 bottom-2 top-2 w-0.5 rounded-r bg-ochre"
                      />
                    )}
                    <Icon size={18} />
                  </Link>
                </TooltipTrigger>
                <TooltipContent side="right">{label}</TooltipContent>
              </Tooltip>
            );
          })}
        </nav>

        {/* Integration status — vertical dots with one tooltip listing
            platforms. Connected = success green, disconnected = ink-faint. */}
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
                  <span
                    key={platform}
                    className={cn(
                      'h-2 w-2 rounded-full',
                      connected ? 'bg-success' : 'bg-ink-faint'
                    )}
                  />
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
                    {PLATFORM_FULL_LABELS[platform]}
                  </li>
                );
              })}
            </ul>
          </TooltipContent>
        </Tooltip>

        {/* Language switcher — compact 40 px wide select that fits the
            56 px rail. Sits directly above logout so locale + identity
            controls live in the same footer cluster. */}
        <LanguageSwitcher className="mb-2 h-8 w-10 px-1" />

        {/* Logout */}
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
