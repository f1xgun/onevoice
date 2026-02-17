'use client'

import { useState } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { toast } from 'sonner'
import { api } from '@/lib/api'
import { PlatformCard } from '@/components/integrations/PlatformCard'
import { ConnectDialog } from '@/components/integrations/ConnectDialog'

interface Integration {
  platform: string
  status: 'active' | 'inactive' | 'error'
  last_sync_at?: string
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
  const [connectingPlatform, setConnectingPlatform] = useState<string | null>(null)

  const { data } = useQuery<Integration[]>({
    queryKey: ['integrations'],
    queryFn: () => api.get('/integrations').then((r) => (r.data.integrations ?? []) as Integration[]),
  })

  const disconnectMutation = useMutation({
    mutationFn: (platform: string) => api.delete(`/integrations/${platform}`),
    onSuccess: () => { qc.invalidateQueries({ queryKey: ['integrations'] }); toast.success('Отключено') },
    onError: () => toast.error('Ошибка отключения'),
  })

  const connectMutation = useMutation({
    mutationFn: ({ platform, credentials }: { platform: string; credentials: Record<string, string> }) =>
      api.post(`/integrations/${platform}/connect`, credentials),
    onSuccess: () => { qc.invalidateQueries({ queryKey: ['integrations'] }); toast.success('Подключено') },
    onError: () => toast.error('Ошибка подключения'),
  })

  const getIntegration = (platformId: string): Integration | undefined =>
    data?.find((i) => i.platform === platformId)

  return (
    <div className="p-8 max-w-3xl">
      <h1 className="text-2xl font-bold mb-6">Интеграции</h1>

      <div className="grid grid-cols-1 md:grid-cols-2 gap-4 mb-8">
        {PLATFORMS.map((p) => {
          const integration = getIntegration(p.id)
          return (
            <PlatformCard
              key={p.id}
              {...p}
              platform={p.id}
              status={integration?.status ?? null}
              lastSyncAt={integration?.last_sync_at}
              onConnect={() => setConnectingPlatform(p.id)}
              onDisconnect={() => disconnectMutation.mutate(p.id)}
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
            status={null}
            disabled
            onConnect={() => {}}
            onDisconnect={() => {}}
          />
        ))}
      </div>

      {connectingPlatform && (
        <ConnectDialog
          platform={connectingPlatform}
          open={true}
          onClose={() => setConnectingPlatform(null)}
          onConnect={(credentials) =>
            connectMutation.mutateAsync({ platform: connectingPlatform, credentials })
          }
        />
      )}
    </div>
  )
}
