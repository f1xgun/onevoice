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

// Permanent icon-only nav-rail (D-14): width 56–64 px (w-14 = 56 px),
// vertical icon column, rendered on every authenticated route. Project
// tree, search slot, and pinned slot live in <ProjectPane> — NOT here.
const navItems = [
  { href: '/chat', label: 'Чат', icon: MessageCircle },
  { href: '/integrations', label: 'Интеграции', icon: Plug },
  { href: '/business', label: 'Бизнес', icon: Building2 },
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

export function NavRail() {
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
        className="flex h-screen w-14 shrink-0 flex-col items-center border-r border-gray-700 bg-gray-900 py-2 text-white"
      >
        {/* Logo (icon-sized) */}
        <Link
          href="/chat"
          aria-label="OneVoice"
          className="mb-4 flex h-10 w-10 items-center justify-center rounded-md bg-gray-800 text-sm font-bold text-white"
        >
          OV
        </Link>

        {/* Vertical nav-list */}
        <nav className="flex flex-1 flex-col gap-1">
          {navItems.map(({ href, label, icon: Icon }) => {
            const isActive = pathname.startsWith(href);
            return (
              <Tooltip key={href}>
                <TooltipTrigger asChild>
                  <Link
                    href={href}
                    aria-label={label}
                    className={cn(
                      'flex h-10 w-10 items-center justify-center rounded-md transition-colors',
                      isActive
                        ? 'bg-gray-700 text-white'
                        : 'text-gray-400 hover:bg-gray-800 hover:text-white'
                    )}
                  >
                    <Icon size={18} />
                  </Link>
                </TooltipTrigger>
                <TooltipContent side="right">{label}</TooltipContent>
              </Tooltip>
            );
          })}
        </nav>

        {/* Integration status — single tooltip listing platforms */}
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
                      connected ? 'bg-green-500' : 'bg-gray-500'
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
                        connected ? 'bg-green-500' : 'bg-gray-500'
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
              className="mb-2 flex h-10 w-10 items-center justify-center rounded-md text-gray-400 hover:bg-gray-800 hover:text-white"
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
