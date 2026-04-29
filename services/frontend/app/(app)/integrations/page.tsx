'use client';

import { useState, useEffect, useRef } from 'react';
import { useSearchParams } from 'next/navigation';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { toast } from 'sonner';
import { api } from '@/lib/api';
import { trackClick } from '@/lib/telemetry';
import { Button } from '@/components/ui/button';
import { PageHeader } from '@/components/ui/page-header';
import { MonoLabel } from '@/components/ui/mono-label';
import { PlatformCard } from '@/components/integrations/PlatformCard';
import { TelegramConnectModal } from '@/components/integrations/TelegramConnectModal';
import { VKCommunityModal } from '@/components/integrations/VKCommunityModal';
import { GoogleLocationModal } from '@/components/integrations/GoogleLocationModal';
import { WhitelistWarningBanner } from '@/components/integrations/WhitelistWarningBanner';
import type { Business } from '@/types/business';

interface Integration {
  id: string;
  platform: string;
  status: 'active' | 'inactive' | 'error' | 'pending_cookies' | 'token_expired';
  externalId: string;
  metadata?: Record<string, unknown>;
  createdAt: string;
}

// MVP-supported platforms — backend can connect, OneVoice can post/read.
// Per design_handoff README v2 §5: Google Business + 2GIS render only in
// the "Скоро" section, never as live integrations.
const PLATFORMS = [
  { id: 'telegram', label: 'Telegram', description: 'Бот для канала и уведомлений' },
  { id: 'vk', label: 'ВКонтакте', description: 'Публикации и комментарии' },
  { id: 'yandex_business', label: 'Яндекс.Бизнес', description: 'Отзывы и информация' },
];

const SOON_PLATFORMS = [
  { id: 'google_business', label: 'Google Business', when: 'оценивается' },
  { id: '2gis', label: '2ГИС', when: 'Q3 2026' },
  { id: 'avito', label: 'Авито', when: 'Q4 2026' },
  { id: 'whatsapp', label: 'WhatsApp', when: 'оценивается' },
];

interface LastRegistered {
  integrationId: string;
  businessId: string;
  platform: string;
}

export default function IntegrationsPage() {
  const qc = useQueryClient();
  const searchParams = useSearchParams();
  const [telegramOpen, setTelegramOpen] = useState(false);
  const [vkCommunityOpen, setVkCommunityOpen] = useState(false);
  const [googleLocationOpen, setGoogleLocationOpen] = useState(false);
  const [lastRegistered, setLastRegistered] = useState<LastRegistered | null>(null);
  const prevIntegrationIdsRef = useRef<Set<string> | null>(null);

  // Handle OAuth callback results
  useEffect(() => {
    const connected = searchParams.get('connected');
    const error = searchParams.get('error');

    if (connected === 'vk') {
      toast.success('VK сообщество подключено');
      qc.invalidateQueries({ queryKey: ['integrations'] });
      window.history.replaceState({}, '', '/integrations');
    }
    if (connected === 'google_business') {
      toast.success('Google Business Profile подключён');
      qc.invalidateQueries({ queryKey: ['integrations'] });
      window.history.replaceState({}, '', '/integrations');
    }

    const googleStep = searchParams.get('google_step');
    if (googleStep === 'select_location') {
      setGoogleLocationOpen(true);
      window.history.replaceState({}, '', '/integrations');
    }

    if (error) {
      const messages: Record<string, string> = {
        missing_params: 'Не получилось войти: не хватает параметров',
        invalid_state: 'Не получилось войти: сессия истекла',
        token_exchange: 'Не удалось обменять токен',
        connect_failed: 'Не удалось подключить',
        no_community_token: 'Не удалось получить токен сообщества',
        internal: 'Что-то пошло не так. Попробуйте ещё раз.',
        no_refresh_token: 'Google не вернул refresh-токен. Попробуйте подключить снова.',
        no_locations: 'В этом аккаунте Google нет бизнес-локаций.',
      };
      toast.error(messages[error] || `Не получилось: ${error}`);
      window.history.replaceState({}, '', '/integrations');
    }
  }, [searchParams, qc]);

  const { data: integrations = [] } = useQuery<Integration[]>({
    queryKey: ['integrations'],
    queryFn: () =>
      api.get('/integrations').then((r) => (Array.isArray(r.data) ? r.data : []) as Integration[]),
  });

  const { data: business } = useQuery<Business>({
    queryKey: ['business'],
    queryFn: () => api.get('/business').then((r) => r.data as Business),
  });

  // Detect newly-registered integrations to show the post-connect banner
  // (whitelist heads-up).
  useEffect(() => {
    const currentIds = new Set(integrations.map((i) => i.id));
    const prev = prevIntegrationIdsRef.current;

    if (prev == null) {
      prevIntegrationIdsRef.current = currentIds;
      return;
    }

    const added = integrations.filter((i) => !prev.has(i.id));
    if (added.length > 0 && business?.id) {
      const latest = added[added.length - 1];
      setLastRegistered({
        integrationId: latest.id,
        businessId: business.id,
        platform: latest.platform,
      });
    }
    prevIntegrationIdsRef.current = currentIds;
  }, [integrations, business?.id]);

  const disconnectMutation = useMutation({
    mutationFn: (integrationId: string) => api.delete(`/integrations/${integrationId}`),
    onSuccess: () => {
      trackClick('disconnect_integration');
      qc.invalidateQueries({ queryKey: ['integrations'] });
      toast.success('Канал отключён');
    },
    onError: () => toast.error('Не получилось отключить'),
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
      setVkCommunityOpen(true);
      return;
    }
    if (platformId === 'google_business') {
      try {
        const { data } = await api.get('/integrations/google_business/auth-url');
        window.location.href = data.url;
      } catch {
        toast.error('Не получилось открыть авторизацию Google');
      }
      return;
    }
    try {
      const { data } = await api.get(`/integrations/${platformId}/auth-url`);
      window.location.href = data.url;
    } catch {
      toast.error('Не получилось открыть авторизацию');
    }
  };

  return (
    <>
      <PageHeader
        title="Интеграции"
        sub="Подключите каналы, по которым с вами общаются клиенты. OneVoice будет принимать в них сообщения и публиковать посты."
        actions={
          <Button variant="ghost" size="md" disabled>
            Журнал событий
          </Button>
        }
      />

      <div className="px-12 pb-16">
        {lastRegistered && (
          <div className="mb-8">
            <WhitelistWarningBanner
              integrationId={lastRegistered.integrationId}
              businessId={lastRegistered.businessId}
              platform={lastRegistered.platform}
            />
          </div>
        )}

        <SectionLabel>Подключённые</SectionLabel>
        <div className="grid grid-cols-1 items-start gap-4 md:grid-cols-2">
          {PLATFORMS.map((p) => {
            const platformIntegrations = getIntegrationsForPlatform(p.id);
            return (
              <PlatformCard
                key={p.id}
                platform={p.id}
                label={p.label}
                description={p.description}
                integrations={platformIntegrations}
                onConnect={() => handleConnect(p.id)}
                onDisconnect={(integrationId) => disconnectMutation.mutate(integrationId)}
              />
            );
          })}
        </div>

        <SectionLabel className="mt-12">Скоро</SectionLabel>
        <div className="grid grid-cols-1 items-start gap-4 md:grid-cols-2 lg:grid-cols-3">
          {SOON_PLATFORMS.map((p) => (
            <SoonCard key={p.id} label={p.label} when={p.when} />
          ))}
        </div>

        <div className="mt-14 flex flex-col items-stretch gap-4 rounded-lg border border-line bg-paper-sunken p-6 sm:flex-row sm:items-center">
          <div className="min-w-0 flex-1">
            <div className="text-base font-medium text-ink">Не нашли свой канал?</div>
            <div className="mt-1 text-sm text-ink-mid">
              Подключите его через наш API или напишите — добавим.
            </div>
          </div>
          <div className="flex shrink-0 gap-2">
            <Button variant="secondary" size="md" disabled>
              Документация API
            </Button>
            <Button variant="ghost" size="md" disabled>
              Запросить канал
            </Button>
          </div>
        </div>
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
    </>
  );
}

function SectionLabel({ children, className }: { children: React.ReactNode; className?: string }) {
  return (
    <div className={`mb-4 mt-2 flex items-center gap-3 ${className ?? ''}`}>
      <MonoLabel>{children}</MonoLabel>
      <span aria-hidden className="h-px flex-1 bg-line-soft" />
    </div>
  );
}

function SoonCard({ label, when }: { label: string; when: string }) {
  return (
    <div className="flex items-center gap-4 rounded-lg border border-dashed border-line bg-paper-raised p-5">
      <span
        aria-hidden
        className="h-10 w-10 shrink-0 rounded-md border border-line-soft bg-paper-sunken"
      />
      <div className="min-w-0 flex-1">
        <div className="text-[15px] font-medium text-ink">{label}</div>
        <MonoLabel className="mt-0.5">{when}</MonoLabel>
      </div>
      <Button variant="ghost" size="sm" disabled>
        Подписаться
      </Button>
    </div>
  );
}
