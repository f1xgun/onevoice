'use client';

import Link from 'next/link';
import { usePathname } from 'next/navigation';
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
import { cn } from '@/lib/utils';
import { useAuthStore } from '@/lib/auth';
import { useQuery } from '@tanstack/react-query';
import { api } from '@/lib/api';

interface Integration {
  platform: string;
  status: string;
  last_sync_at?: string;
}

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

export function Sidebar() {
  const pathname = usePathname();
  const logout = useAuthStore((s) => s.logout);
  const user = useAuthStore((s) => s.user);

  const { data: integrations } = useQuery<Integration[]>({
    queryKey: ['integrations'],
    queryFn: () =>
      api.get('/integrations').then((r) => (r.data.integrations ?? []) as Integration[]),
  });

  return (
    <aside className="flex h-screen w-60 shrink-0 flex-col bg-gray-900 text-white">
      {/* Logo */}
      <div className="border-b border-gray-700 p-4">
        <h1 className="text-xl font-bold text-white">OneVoice</h1>
        <p className="truncate text-xs text-gray-400">{user?.email}</p>
      </div>

      {/* Navigation */}
      <nav className="flex-1 space-y-1 p-2">
        {navItems.map(({ href, label, icon: Icon }) => (
          <Link
            key={href}
            href={href}
            className={cn(
              'flex items-center gap-3 rounded-md px-3 py-2 text-sm transition-colors',
              pathname.startsWith(href)
                ? 'bg-gray-700 text-white'
                : 'text-gray-300 hover:bg-gray-800 hover:text-white'
            )}
          >
            <Icon size={18} />
            {label}
          </Link>
        ))}
      </nav>

      {/* Platform status */}
      <div className="space-y-2 border-t border-gray-700 p-4">
        <p className="text-xs uppercase tracking-wide text-gray-500">Платформы</p>
        {['telegram', 'vk', 'yandex_business'].map((platform) => {
          const integration = integrations?.find((i) => i.platform === platform);
          const connected = integration?.status === 'active';
          return (
            <div key={platform} className="flex items-center gap-2 text-xs text-gray-300">
              <span
                className="h-2 w-2 rounded-full"
                style={{ backgroundColor: connected ? '#22c55e' : '#6b7280' }}
              />
              {platformLabels[platform]}
            </div>
          );
        })}
      </div>

      {/* Logout */}
      <button
        onClick={logout}
        className="flex items-center gap-3 border-t border-gray-700 p-4 text-sm text-gray-400 hover:bg-gray-800 hover:text-white"
      >
        <LogOut size={18} />
        Выйти
      </button>
    </aside>
  );
}
