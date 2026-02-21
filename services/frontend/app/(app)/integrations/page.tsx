'use client'

import { useState } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { toast } from 'sonner'
import { api } from '@/lib/api'
import { PlatformCard } from '@/components/integrations/PlatformCard'
import { TelegramConnectModal } from '@/components/integrations/TelegramConnectModal'

interface Integration {
  id: string
  platform: string
  status: 'active' | 'inactive' | 'error' | 'pending_cookies' | 'token_expired'
  externalId: string
  metadata?: Record<string, unknown>
  createdAt: string
}

const PLATFORMS = [
  { id: 'telegram', label: 'Telegram', description: 'Бот для канала и уведомлений', color: '#2AABEE' },
  { id: 'vk', label: 'ВКонтакте', description: 'Публикации и комментарии', color: '#4680C2' },
  { id: 'yandex_business', label: 'Яндекс.Бизнес', description: 'Отзывы и информация', color: '#FC3F1D' },
]

const DISABLED_PLATFORMS = [
  { id: '2gis', label: '2ГИС', description: 'Скоро (Phase 2)', color: '#1DA045' },
  { id: 'avito', label: 'Авито', description: 'Скоро (Phase 2)', color: '#00AAFF' },
  { id: 'google', label: 'Google Business', description: 'Скоро (Phase 3)', color: '#4285F4' },
]

export default function IntegrationsPage() {
  const qc = useQueryClient()
  const [telegramOpen, setTelegramOpen] = useState(false)

  const { data: integrations = [] } = useQuery<Integration[]>({
    queryKey: ['integrations'],
    queryFn: () => api.get('/integrations').then((r) => (Array.isArray(r.data) ? r.data : []) as Integration[]),
  })

  const disconnectMutation = useMutation({
    mutationFn: (integrationId: string) => api.delete(`/integrations/${integrationId}`),
    onSuccess: () => { qc.invalidateQueries({ queryKey: ['integrations'] }); toast.success('Отключено') },
    onError: () => toast.error('Ошибка отключения'),
  })

  const getIntegrationsForPlatform = (platformId: string): Integration[] =>
    integrations.filter((i) => i.platform === platformId)

  const handleConnect = async (platformId: string) => {
    if (platformId === 'telegram') {
      setTelegramOpen(true)
      return
    }

    // VK and Yandex.Business: OAuth redirect flow
    try {
      const { data } = await api.get(`/integrations/${platformId}/auth-url`)
      window.location.href = data.url
    } catch {
      toast.error('Ошибка получения ссылки авторизации')
    }
  }

  return (
    <div className="p-8 max-w-3xl">
      <h1 className="text-2xl font-bold mb-6">Интеграции</h1>

      <div className="grid grid-cols-1 md:grid-cols-2 gap-4 mb-8">
        {PLATFORMS.map((p) => {
          const platformIntegrations = getIntegrationsForPlatform(p.id)
          return (
            <PlatformCard
              key={p.id}
              {...p}
              platform={p.id}
              integrations={platformIntegrations}
              onConnect={() => handleConnect(p.id)}
              onDisconnect={(integrationId) => disconnectMutation.mutate(integrationId)}
            />
          )
        })}
      </div>

      <h2 className="text-lg font-medium text-gray-400 mb-4">Скоро</h2>
      <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
        {DISABLED_PLATFORMS.map((p) => (
          <PlatformCard
            key={p.id}
            {...p}
            platform={p.id}
            integrations={[]}
            disabled
            onConnect={() => {}}
            onDisconnect={() => {}}
          />
        ))}
      </div>

      <TelegramConnectModal
        open={telegramOpen}
        onClose={() => { setTelegramOpen(false); qc.invalidateQueries({ queryKey: ['integrations'] }) }}
      />
    </div>
  )
}
