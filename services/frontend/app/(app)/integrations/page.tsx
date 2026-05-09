'use client';

import { useState, useEffect, useRef } from 'react';
import { useSearchParams } from 'next/navigation';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { useTranslations } from 'next-intl';
import { toast } from 'sonner';
import { api } from '@/lib/api';
import { API_PATHS } from '@/lib/constants/apiPaths';
import { QUERY_KEYS } from '@/lib/constants/queryKeys';
import { trackClick } from '@/lib/telemetry';
import { Button } from '@/components/ui/button';
import { PageHeader } from '@/components/ui/page-header';
import { MonoLabel } from '@/components/ui/mono-label';
import { EmptyChannels, SkeletonChannels } from '@/components/states';
import { PlatformCard } from '@/components/integrations/PlatformCard';
import { TelegramConnectModal } from '@/components/integrations/TelegramConnectModal';
import { VKCommunityModal } from '@/components/integrations/VKCommunityModal';
import { GoogleLocationModal } from '@/components/integrations/GoogleLocationModal';
import { YandexBusinessConnectModal } from '@/components/integrations/YandexBusinessConnectModal';
import { WhitelistWarningBanner } from '@/components/integrations/WhitelistWarningBanner';
import { usePlatforms } from '@/lib/hooks/usePlatforms';
import type { Business } from '@/types/business';

interface Integration {
  id: string;
  platform: string;
  status: 'active' | 'inactive' | 'error' | 'pending_cookies' | 'token_expired';
  externalId: string;
  metadata?: Record<string, unknown>;
  createdAt: string;
}

interface LastRegistered {
  integrationId: string;
  businessId: string;
  platform: string;
}

export default function IntegrationsPage() {
  const qc = useQueryClient();
  const tIntegrations = useTranslations('integrations');
  const searchParams = useSearchParams();
  const [telegramOpen, setTelegramOpen] = useState(false);
  const [vkCommunityOpen, setVkCommunityOpen] = useState(false);
  const [googleLocationOpen, setGoogleLocationOpen] = useState(false);
  const [yandexOpen, setYandexOpen] = useState(false);
  const [lastRegistered, setLastRegistered] = useState<LastRegistered | null>(null);
  const prevIntegrationIdsRef = useRef<Set<string> | null>(null);

  // Platforms come from GET /api/v1/platforms (backed by pkg/domain/platform.go)
  // — single source of truth for what we expose. status="oauth_not_configured"
  // entries are hidden so we never advertise a flow that would dead-end at
  // missing-creds. coming_soon entries render as the marketing teaser cards.
  const { platforms } = usePlatforms();
  const activePlatforms = platforms.filter((p) => p.status === 'active');
  const comingSoonPlatforms = platforms.filter((p) => p.status === 'coming_soon');

  // Handle OAuth callback results
  useEffect(() => {
    const connected = searchParams.get('connected');
    const error = searchParams.get('error');

    if (connected === 'vk') {
      toast.success(tIntegrations('vkConnected'));
      qc.invalidateQueries({ queryKey: QUERY_KEYS.INTEGRATIONS });
      window.history.replaceState({}, '', API_PATHS.INTEGRATIONS.ROOT);
    }
    if (connected === 'google_business') {
      toast.success(tIntegrations('googleConnected'));
      qc.invalidateQueries({ queryKey: QUERY_KEYS.INTEGRATIONS });
      window.history.replaceState({}, '', API_PATHS.INTEGRATIONS.ROOT);
    }

    const googleStep = searchParams.get('google_step');
    if (googleStep === 'select_location') {
      setGoogleLocationOpen(true);
      window.history.replaceState({}, '', API_PATHS.INTEGRATIONS.ROOT);
    }

    if (error) {
      // Server emits snake_case slugs; map to camelCase keys under
      // integrations.oauthErrors. Unknown slugs render the templated
      // fallback ("Не получилось: <slug>") so the user still gets a
      // hint while we add the missing translation.
      const oauthErrorKeyMap: Record<string, string> = {
        missing_params: 'missingParams',
        invalid_state: 'invalidState',
        token_exchange: 'tokenExchange',
        connect_failed: 'connectFailed',
        no_community_token: 'noCommunityToken',
        internal: 'internal',
        no_refresh_token: 'noRefreshToken',
        no_locations: 'noLocations',
      };
      const key = oauthErrorKeyMap[error];
      const message = key
        ? tIntegrations(`oauthErrors.${key}`)
        : tIntegrations('oauthErrors.fallback', { error });
      toast.error(message);
      window.history.replaceState({}, '', API_PATHS.INTEGRATIONS.ROOT);
    }
  }, [searchParams, qc, tIntegrations]);

  const { data: integrations = [], isLoading: integrationsLoading } = useQuery<Integration[]>({
    queryKey: QUERY_KEYS.INTEGRATIONS,
    queryFn: () =>
      api
        .get(API_PATHS.INTEGRATIONS.ROOT)
        .then((r) => (Array.isArray(r.data) ? r.data : []) as Integration[]),
  });

  const { data: business } = useQuery<Business>({
    queryKey: QUERY_KEYS.BUSINESS,
    queryFn: () => api.get(API_PATHS.BUSINESS.ROOT).then((r) => r.data as Business),
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
      qc.invalidateQueries({ queryKey: QUERY_KEYS.INTEGRATIONS });
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
        const { data } = await api.get(API_PATHS.INTEGRATIONS.GOOGLE_AUTH_URL);
        window.location.href = data.url;
      } catch {
        toast.error('Не получилось открыть авторизацию Google');
      }
      return;
    }
    if (platformId === 'yandex_business') {
      // Cookie-paste flow — Yandex has no public OAuth API for the actions
      // we automate, so the agent needs real session cookies.
      setYandexOpen(true);
    }
  };

  return (
    <>
      <PageHeader title={tIntegrations('title')} sub={tIntegrations('subtitle')} />

      <div className="px-4 pb-10 sm:px-12 sm:pb-16">
        {lastRegistered && (
          <div className="mb-8">
            <WhitelistWarningBanner
              integrationId={lastRegistered.integrationId}
              businessId={lastRegistered.businessId}
              platform={lastRegistered.platform}
            />
          </div>
        )}

        <SectionLabel>{tIntegrations('page.connected')}</SectionLabel>
        {integrationsLoading ? (
          // Static paper-sunken skeletons per Linen loading rule (no shimmer).
          <SkeletonChannels count={3} />
        ) : integrations.length === 0 ? (
          // First-run state per mock-states.jsx "Каналы не подключены" — single
          // ochre-emphasis CTA scrolls to the platform list below so the user
          // can pick where to start.
          <EmptyChannels
            onConnect={() => {
              const target = document.getElementById('integrations-platform-grid');
              if (target) {
                target.scrollIntoView({ behavior: 'smooth', block: 'start' });
              }
            }}
          />
        ) : null}
        <div
          id="integrations-platform-grid"
          className="grid grid-cols-1 items-start gap-4 md:grid-cols-2"
        >
          {activePlatforms.map((p) => {
            const platformIntegrations = getIntegrationsForPlatform(p.id);
            return (
              <PlatformCard
                key={p.id}
                platform={p.id}
                label={p.fullLabel}
                description={p.description}
                integrations={platformIntegrations}
                onConnect={() => handleConnect(p.id)}
                onDisconnect={(integrationId) => disconnectMutation.mutate(integrationId)}
              />
            );
          })}
        </div>

        {comingSoonPlatforms.length > 0 && (
          <>
            <SectionLabel className="mt-12">{tIntegrations('page.comingSoon')}</SectionLabel>
            <div className="grid grid-cols-1 items-start gap-4 md:grid-cols-2 lg:grid-cols-3">
              {comingSoonPlatforms.map((p) => (
                <SoonCard key={p.id} label={p.fullLabel} when={p.comingSoonWhen ?? 'скоро'} />
              ))}
            </div>
          </>
        )}

        <div className="mt-14 flex flex-col items-stretch gap-4 rounded-lg border border-line bg-paper-sunken p-6 sm:flex-row sm:items-center">
          <div className="min-w-0 flex-1">
            <div className="text-base font-medium text-ink">
              {tIntegrations('page.missingPlatform')}
            </div>
            <div className="mt-1 text-sm text-ink-mid">
              {tIntegrations('page.missingPlatformBody')}
            </div>
          </div>
          <div className="flex shrink-0 gap-2">
            <Button variant="secondary" size="md" asChild>
              <a href="mailto:hello@onevoice.app?subject=Запрос%20канала">
                {tIntegrations('page.requestChannel')}
              </a>
            </Button>
          </div>
        </div>
      </div>

      <TelegramConnectModal
        open={telegramOpen}
        onClose={() => {
          setTelegramOpen(false);
          qc.invalidateQueries({ queryKey: QUERY_KEYS.INTEGRATIONS });
        }}
      />

      <VKCommunityModal
        open={vkCommunityOpen}
        onClose={() => {
          setVkCommunityOpen(false);
          qc.invalidateQueries({ queryKey: QUERY_KEYS.INTEGRATIONS });
        }}
      />

      <GoogleLocationModal
        open={googleLocationOpen}
        onClose={() => {
          setGoogleLocationOpen(false);
          qc.invalidateQueries({ queryKey: QUERY_KEYS.INTEGRATIONS });
        }}
      />

      <YandexBusinessConnectModal open={yandexOpen} onClose={() => setYandexOpen(false)} />
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
  const tIntegrations = useTranslations('integrations');
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
        {tIntegrations('page.subscribe')}
      </Button>
    </div>
  );
}
