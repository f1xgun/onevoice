'use client'

import Link from 'next/link'
import { usePathname } from 'next/navigation'
import { MessageCircle, Plug, Building2, Star, FileText, ListTodo, Settings, LogOut } from 'lucide-react'
import { cn } from '@/lib/utils'
import { useAuthStore } from '@/lib/auth'
import { useQuery } from '@tanstack/react-query'
import { api } from '@/lib/api'

interface Integration {
  platform: string
  status: string
  last_sync_at?: string
}

const navItems = [
  { href: '/chat', label: 'Чат', icon: MessageCircle },
  { href: '/integrations', label: 'Интеграции', icon: Plug },
  { href: '/business', label: 'Бизнес', icon: Building2 },
  { href: '/reviews', label: 'Отзывы', icon: Star },
  { href: '/posts', label: 'Посты', icon: FileText },
  { href: '/tasks', label: 'Задачи', icon: ListTodo },
  { href: '/settings', label: 'Настройки', icon: Settings },
]

const platformLabels: Record<string, string> = {
  telegram: 'Telegram',
  vk: 'ВКонтакте',
  yandex_business: 'Яндекс.Бизнес',
}

export function Sidebar() {
  const pathname = usePathname()
  const logout = useAuthStore((s) => s.logout)
  const user = useAuthStore((s) => s.user)

  const { data: integrations } = useQuery<Integration[]>({
    queryKey: ['integrations'],
    queryFn: () => api.get('/integrations').then((r) => (r.data.integrations ?? []) as Integration[]),
  })

  return (
    <aside className="w-60 h-screen bg-gray-900 text-white flex flex-col shrink-0">
      {/* Logo */}
      <div className="p-4 border-b border-gray-700">
        <h1 className="text-xl font-bold text-white">OneVoice</h1>
        <p className="text-xs text-gray-400 truncate">{user?.email}</p>
      </div>

      {/* Navigation */}
      <nav className="flex-1 p-2 space-y-1">
        {navItems.map(({ href, label, icon: Icon }) => (
          <Link
            key={href}
            href={href}
            className={cn(
              'flex items-center gap-3 px-3 py-2 rounded-md text-sm transition-colors',
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
      <div className="p-4 border-t border-gray-700 space-y-2">
        <p className="text-xs text-gray-500 uppercase tracking-wide">Платформы</p>
        {['telegram', 'vk', 'yandex_business'].map((platform) => {
          const integration = integrations?.find((i) => i.platform === platform)
          const connected = integration?.status === 'active'
          return (
            <div key={platform} className="flex items-center gap-2 text-xs text-gray-300">
              <span
                className="w-2 h-2 rounded-full"
                style={{ backgroundColor: connected ? '#22c55e' : '#6b7280' }}
              />
              {platformLabels[platform]}
            </div>
          )
        })}
      </div>

      {/* Logout */}
      <button
        onClick={logout}
        className="flex items-center gap-3 p-4 text-gray-400 hover:text-white hover:bg-gray-800 text-sm border-t border-gray-700"
      >
        <LogOut size={18} />
        Выйти
      </button>
    </aside>
  )
}
