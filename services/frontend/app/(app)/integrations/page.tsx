'use client';

import { useState, useEffect } from 'react';
import { useSearchParams } from 'next/navigation';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { toast } from 'sonner';
import { api } from '@/lib/api';
import { trackClick } from '@/lib/telemetry';
import { PlatformCard } from '@/components/integrations/PlatformCard';
import { TelegramConnectModal } from '@/components/integrations/TelegramConnectModal';
import { VKCommunityModal } from '@/components/integrations/VKCommunityModal';
import { GoogleLocationModal } from '@/components/integrations/GoogleLocationModal';

interface Integration {
  id: string;
  platform: string;
  status: 'active' | 'inactive' | 'error' | 'pending_cookies' | 'token_expired';
  externalId: string;
  metadata?: Record<string, unknown>;
  createdAt: string;
}

const PLATFORMS = [
  {
    id: 'telegram',
    label: 'Telegram',
    description: 'Бот для канала и уведомлений',
    color: '#2AABEE',
  },
  { id: 'vk', label: 'ВКонтакте', description: 'Публикации и комментарии', color: '#4680C2' },
  {
    id: 'yandex_business',
    label: 'Яндекс.Бизнес',
    description: 'Отзывы и информация',
    color: '#FC3F1D',
  },
  {
    id: 'google_business',
    label: 'Google Business',
    description: 'Отзывы и информация о бизнесе',
    color: '#4285F4',
  },
];

const DISABLED_PLATFORMS = [
  { id: '2gis', label: '2ГИС', description: 'Скоро', color: '#1DA045' },
  { id: 'avito', label: 'Авито', description: 'Скоро', color: '#00AAFF' },
];

export default function IntegrationsPage() {
  const qc = useQueryClient();
  const searchParams = useSearchParams();
  const [telegramOpen, setTelegramOpen] = useState(false);
  const [vkCommunityOpen, setVkCommunityOpen] = useState(false);
  const [googleLocationOpen, setGoogleLocationOpen] = useState(false);

  // Handle OAuth callback results
  useEffect(() => {
    const connected = searchParams.get('connected');
    const error = searchParams.get('error');

    if (connected === 'vk') {
      toast.success('VK сообщество подключено!');
      qc.invalidateQueries({ queryKey: ['integrations'] });
      window.history.replaceState({}, '', '/integrations');
    }
    if (connected === 'google_business') {
      toast.success('Google Business Profile подключен!');
      qc.invalidateQueries({ queryKey: ['integrations'] });
      window.history.replaceState({}, '', '/integrations');
    }

    const googleStep = searchParams.get('google_step');
    if (googleStep === 'select_location') {
      setGoogleLocationOpen(true);
      window.history.replaceState({}, '', '/integrations');
    }

    const vkStep = searchParams.get('vk_step');
    if (vkStep === 'select_community') {
      setVkCommunityOpen(true);
      window.history.replaceState({}, '', '/integrations');
    }

    if (error) {
      const messages: Record<string, string> = {
        missing_params: 'Ошибка авторизации: отсутствуют параметры',
        invalid_state: 'Ошибка авторизации: невалидная сессия',
        token_exchange: 'Ошибка обмена токена',
        connect_failed: 'Ошибка подключения интеграции',
        no_community_token: 'Не удалось получить токен сообщества',
        internal: 'Внутренняя ошибка. Попробуйте ещё раз.',
        no_refresh_token: 'Ошибка авторизации Google: не получен refresh token. Попробуйте снова.',
        no_locations: 'Не найдены бизнес-локации в вашем аккаунте Google.',
      };
      toast.error(messages[error] || `Ошибка: ${error}`);
      window.history.replaceState({}, '', '/integrations');
    }
  }, [searchParams, qc]);

  const { data: integrations = [] } = useQuery<Integration[]>({
    queryKey: ['integrations'],
    queryFn: () =>
      api.get('/integrations').then((r) => (Array.isArray(r.data) ? r.data : []) as Integration[]),
  });

  const disconnectMutation = useMutation({
    mutationFn: (integrationId: string) => api.delete(`/integrations/${integrationId}`),
    onSuccess: () => {
      trackClick('disconnect_integration');
      qc.invalidateQueries({ queryKey: ['integrations'] });
      toast.success('Отключено');
    },
    onError: () => toast.error('Ошибка отключения'),
  });

  const getIntegrationsForPlatform = (platformId: string): Integration[] =>
    integrations.filter((i) => i.platform === platformId);

  const handleConnect = async (platformId: string) => {
    trackClick('connect_integration', { platform: platformId });
    if (platformId === 'telegram') {
      setTelegramOpen(true);
      return;
    }

    if (platformId === 'vk') {
      // Start VK ID (PKCE) OAuth; callback stores the user token in Redis
      // and redirects back with vk_step=select_community, which opens
      // VKCommunityModal to pick the community for the community-scoped
      // second OAuth hop.
      try {
        const { data } = await api.get('/integrations/vk/auth-url');
        window.location.href = data.url;
      } catch {
        toast.error('Ошибка получения ссылки авторизации VK');
      }
      return;
    }

    if (platformId === 'google_business') {
      try {
        const { data } = await api.get('/integrations/google_business/auth-url');
        window.location.href = data.url;
      } catch {
        toast.error('Ошибка получения ссылки авторизации Google');
      }
      return;
    }

    // Yandex.Business: OAuth redirect flow
    try {
      const { data } = await api.get(`/integrations/${platformId}/auth-url`);
      window.location.href = data.url;
    } catch {
      toast.error('Ошибка получения ссылки авторизации');
    }
  };

  return (
    <div className="max-w-5xl p-8">
      <h1 className="mb-6 text-2xl font-bold">Интеграции</h1>

      <div className="mb-8 grid grid-cols-1 items-start gap-4 md:grid-cols-2">
        {PLATFORMS.map((p) => {
          const platformIntegrations = getIntegrationsForPlatform(p.id);
          return (
            <PlatformCard
              key={p.id}
              {...p}
              platform={p.id}
              integrations={platformIntegrations}
              onConnect={() => handleConnect(p.id)}
              onDisconnect={(integrationId) => disconnectMutation.mutate(integrationId)}
            />
          );
        })}
      </div>

      <h2 className="mb-4 text-lg font-medium text-gray-400">Скоро</h2>
      <div className="grid grid-cols-1 items-start gap-4 md:grid-cols-2">
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
        onClose={() => {
          setTelegramOpen(false);
          qc.invalidateQueries({ queryKey: ['integrations'] });
        }}
      />

      <VKCommunityModal
        open={vkCommunityOpen}
        onClose={() => {
          setVkCommunityOpen(false);
          qc.invalidateQueries({ queryKey: ['integrations'] });
        }}
      />

      <GoogleLocationModal
        open={googleLocationOpen}
        onClose={() => {
          setGoogleLocationOpen(false);
          qc.invalidateQueries({ queryKey: ['integrations'] });
        }}
      />
    </div>
  );
}
