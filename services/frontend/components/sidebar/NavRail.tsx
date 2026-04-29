'use client';

import Link from 'next/link';
import { usePathname, useRouter } from 'next/navigation';
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
import { cn } from '@/lib/utils';
import { useAuthStore } from '@/lib/auth';
import { api } from '@/lib/api';

interface Integration {
  platform: string;
  status: string;
  last_sync_at?: string;
}

// Permanent icon-only nav-rail (D-14): width 56–64 px, vertical icon column,
// rendered on every authenticated route. Project tree, search, and pinned
// rows live in <ProjectPane> — NOT here.
//
// Order locked by design handoff README v2 §5: Чат → Интеграции → Профиль
// бизнеса → Отзывы → Посты → Задачи → Настройки.
const navItems = [
  { href: '/chat', label: 'Чат', icon: MessageCircle },
  { href: '/integrations', label: 'Интеграции', icon: Plug },
  { href: '/business', label: 'Профиль бизнеса', icon: Building2 },
  { href: '/reviews', label: 'Отзывы', icon: Star },
  { href: '/posts', label: 'Посты', icon: FileText },
  { href: '/tasks', label: 'Задачи', icon: ListTodo },
  { href: '/settings', label: 'Настройки', icon: Settings },
];

const platformLabels: Record<string, string> = {
  telegram: 'Telegram',
  vk: 'ВКонтакте',
  yandex_business: 'Яндекс.Бизнес',
};

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
  const logout = useAuthStore((s) => s.logout);

  const { data: integrations } = useQuery<Integration[]>({
    queryKey: ['integrations'],
    queryFn: () =>
      api.get('/integrations').then((r) => (Array.isArray(r.data) ? r.data : []) as Integration[]),
    retry: false,
    placeholderData: [],
  });

  function handleLogout() {
    logout();
    router.push('/login');
  }

  return (
    <TooltipProvider delayDuration={150}>
      <aside
        data-testid="nav-rail"
        className="flex h-screen w-14 shrink-0 flex-col items-center border-r border-line bg-paper-raised py-2"
      >
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
          {navItems.map(({ href, label, icon: Icon }) => {
            const isActive = pathname.startsWith(href);
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
                      isActive
                        ? 'text-ink'
                        : 'text-ink-soft hover:bg-paper-sunken hover:text-ink'
                    )}
                  >
                    {isActive && (
                      <span
                        aria-hidden
                        className="absolute -left-2 top-2 bottom-2 w-0.5 rounded-r bg-ochre"
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
              aria-label="Платформы"
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
                    {platformLabels[platform]}
                  </li>
                );
              })}
            </ul>
          </TooltipContent>
        </Tooltip>

        {/* Logout */}
        <Tooltip>
          <TooltipTrigger asChild>
            <button
              type="button"
              onClick={handleLogout}
              aria-label="Выйти"
              className="mb-2 flex h-10 w-10 items-center justify-center rounded-md text-ink-soft transition-colors hover:bg-paper-sunken hover:text-ink"
            >
              <LogOut size={18} />
            </button>
          </TooltipTrigger>
          <TooltipContent side="right">Выйти</TooltipContent>
        </Tooltip>
      </aside>
    </TooltipProvider>
  );
}
