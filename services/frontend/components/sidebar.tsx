'use client';

import { useState } from 'react';
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
  Menu,
} from 'lucide-react';
import { useQuery } from '@tanstack/react-query';
import { Sheet, SheetContent, SheetTrigger } from '@/components/ui/sheet';
import { Button } from '@/components/ui/button';
import { cn } from '@/lib/utils';
import { useAuthStore } from '@/lib/auth';
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

function SidebarContent({ onNavigate }: { onNavigate?: () => void }) {
  const pathname = usePathname();
  const logout = useAuthStore((s) => s.logout);
  const user = useAuthStore((s) => s.user);

  const { data: integrations } = useQuery<Integration[]>({
    queryKey: ['integrations'],
    queryFn: () =>
      api.get('/integrations').then((r) => (Array.isArray(r.data) ? r.data : []) as Integration[]),
    retry: false,
    placeholderData: [],
  });

  return (
    <div className="flex h-full flex-col bg-gray-900 text-white">
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
            onClick={onNavigate}
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
                className={cn('h-2 w-2 rounded-full', connected ? 'bg-green-500' : 'bg-gray-500')}
              />
              {platformLabels[platform]}
            </div>
          );
        })}
      </div>

      {/* Logout */}
      <button
        onClick={() => {
          onNavigate?.();
          logout();
        }}
        className="flex items-center gap-3 border-t border-gray-700 p-4 text-sm text-gray-400 hover:bg-gray-800 hover:text-white"
      >
        <LogOut size={18} />
        Выйти
      </button>
    </div>
  );
}

export function Sidebar() {
  const [open, setOpen] = useState(false);

  return (
    <>
      {/* Mobile top bar */}
      <div className="sticky top-0 z-40 flex h-14 items-center gap-4 border-b bg-background px-4 md:hidden">
        <Sheet open={open} onOpenChange={setOpen}>
          <SheetTrigger asChild>
            <Button variant="ghost" size="icon" className="md:hidden">
              <Menu className="h-5 w-5" />
            </Button>
          </SheetTrigger>
          <SheetContent side="left" className="w-60 p-0">
            <SidebarContent onNavigate={() => setOpen(false)} />
          </SheetContent>
        </Sheet>
        <span className="text-lg font-semibold">OneVoice</span>
      </div>

      {/* Desktop sidebar */}
      <aside className="hidden h-screen w-60 shrink-0 flex-col md:flex">
        <SidebarContent />
      </aside>
    </>
  );
}
