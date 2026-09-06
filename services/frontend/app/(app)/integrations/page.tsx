'use client';

import { useState, useEffect, useRef } from 'react';
import { useSearchParams } from 'next/navigation';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { useTranslations } from 'next-intl';
import { toast } from 'sonner';
import { bizApi } from '@/lib/api/business-api';
import { API_PATHS } from '@/lib/constants/apiPaths';
import { BIZ_API_PATHS, INTEGRATION_ENDPOINTS } from '@/lib/constants/bizApiPaths';
import { QUERY_KEYS } from '@/lib/constants/queryKeys';
import type { IntegrationStatus } from '@/lib/constants/integrationStatus';
import { useBusinessStore } from '@/lib/stores/business';
import { trackClick } from '@/lib/telemetry';
import { Button } from '@/components/ui/button';
import { PageHeader } from '@/components/ui/page-header';
import { MonoLabel } from '@/components/ui/mono-label';
import { EmptyChannels, SkeletonChannels } from '@/components/states';
import { InlineEmpty } from '@/components/states/InlineEmpty';
import { ListLoadError } from '@/components/lists/ListLoadError';
import { PlatformCard } from '@/components/integrations/PlatformCard';
import { TelegramConnectModal } from '@/components/integrations/TelegramConnectModal';
import { VKCommunityModal } from '@/components/integrations/VKCommunityModal';
import { VKCommunityPickerModal } from '@/components/integrations/VKCommunityPickerModal';
import { GoogleLocationModal } from '@/components/integrations/GoogleLocationModal';
import { YandexBusinessConnectModal } from '@/components/integrations/YandexBusinessConnectModal';
import { WhitelistWarningBanner } from '@/components/integrations/WhitelistWarningBanner';
import { SectionHelp } from '@/components/onboarding/SectionHelp';
import { FirstActionWizard } from '@/components/onboarding/FirstActionWizard';
import { IntegrationsSyncPanel } from '@/components/integrations/IntegrationsSyncPanel';
import { usePlatforms } from '@/lib/hooks/usePlatforms';
import { usePermission } from '@/lib/hooks/usePermission';
import type { PlatformId } from '@/lib/platforms';
import type { Business } from '@/types/business';

type ModalPlatform = Extract<PlatformId, 'telegram' | 'vk' | 'google_business' | 'yandex_business'>;

interface ModalDispatchProps {
  open: boolean;
  onClose: () => void;
}

const MODAL_COMPONENTS: Record<ModalPlatform, React.ComponentType<ModalDispatchProps>> = {
  telegram: TelegramConnectModal,
  vk: VKCommunityModal,
  google_business: GoogleLocationModal,
  yandex_business: YandexBusinessConnectModal,
};

// Telegram/VK/Google flows refetch the integration list on close so the new
// row shows up; Yandex's cookie-paste modal manages its own invalidation.
const MODAL_INVALIDATES_ON_CLOSE: Record<ModalPlatform, boolean> = {
  telegram: true,
  vk: true,
  google_business: true,
  yandex_business: false,
};

interface Integration {
  id: string;
  platform: string;
  status: IntegrationStatus;
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
  const tPlatforms = useTranslations('platforms');
  const tPlatformDesc = useTranslations('platforms.description');
  const searchParams = useSearchParams();
  const activeBusinessId = useBusinessStore((s) => s.activeBusinessId);
  const canConnect = usePermission('integrations.connect').allowed;
  const canDisconnect = usePermission('integrations.disconnect').allowed;
  const [activeModalPlatform, setActiveModalPlatform] = useState<ModalPlatform | null>(null);
  const [vkPickerOpen, setVkPickerOpen] = useState(false);
  const [lastRegistered, setLastRegistered] = useState<LastRegistered | null>(null);
  const [wizardOpen, setWizardOpen] = useState(false);
  const prevIntegrationIdsRef = useRef<Set<string> | null>(null);
  const baselineBusinessIdRef = useRef<string | null>(null);

  const { platforms } = usePlatforms();
  const activePlatforms = platforms.filter((p) => p.status === 'active');
  const comingSoonPlatforms = platforms.filter((p) => p.status === 'coming_soon');

  useEffect(() => {
    const connected = searchParams.get('connected');
    const error = searchParams.get('error');

    if (connected === 'vk') {
      toast.success(tIntegrations('vkConnected'));
      qc.invalidateQueries({ queryKey: QUERY_KEYS.BUSINESS_INTEGRATIONS(activeBusinessId) });
      window.history.replaceState({}, '', API_PATHS.INTEGRATIONS.ROOT);
    }
    if (connected === 'google_business') {
      toast.success(tIntegrations('googleConnected'));
      qc.invalidateQueries({ queryKey: QUERY_KEYS.BUSINESS_INTEGRATIONS(activeBusinessId) });
      window.history.replaceState({}, '', API_PATHS.INTEGRATIONS.ROOT);
    }

    // After a fresh connect, ?connected=<platform>&wizard=1 auto-opens the
    // first-action wizard so a new organization lands on a real AI action fast.
    // Additive: the toasts + cache invalidation above still fire; this only
    // opens the wizard when the wizard flag is explicitly present.
    if (connected && searchParams.get('wizard') === '1') {
      setWizardOpen(true);
      window.history.replaceState({}, '', API_PATHS.INTEGRATIONS.ROOT);
    }

    const googleStep = searchParams.get('google_step');
    if (googleStep === 'select_location') {
      setActiveModalPlatform('google_business');
      window.history.replaceState({}, '', API_PATHS.INTEGRATIONS.ROOT);
    }

    const vkStep = searchParams.get('vk_step');
    if (vkStep === 'select_community') {
      setVkPickerOpen(true);
      window.history.replaceState({}, '', API_PATHS.INTEGRATIONS.ROOT);
    }

    const reconnect = searchParams.get('reconnect');
    if (reconnect && reconnect in MODAL_COMPONENTS) {
      setActiveModalPlatform(reconnect as ModalPlatform);
      window.history.replaceState({}, '', API_PATHS.INTEGRATIONS.ROOT);
    }

    if (error) {
      const oauthErrorKeyMap: Record<string, string> = {
        missing_params: 'missingParams',
        invalid_state: 'invalidState',
        token_exchange: 'tokenExchange',
        connect_failed: 'connectFailed',
        already_connected: 'alreadyConnected',
        no_community_token: 'noCommunityToken',
        internal: 'internal',
        no_refresh_token: 'noRefreshToken',
        no_locations: 'noLocations',
        email_verification_required: 'emailVerificationRequired',
        account_pending_deletion: 'accountPendingDeletion',
      };
      const key = oauthErrorKeyMap[error];
      const message = key
        ? tIntegrations(`oauthErrors.${key}`)
        : tIntegrations('oauthErrors.fallback', { error });
      toast.error(message);
      window.history.replaceState({}, '', API_PATHS.INTEGRATIONS.ROOT);
    }
  }, [searchParams, qc, activeBusinessId, tIntegrations]);

  const {
    data: integrations = [],
    isLoading: integrationsLoading,
    isError: integrationsError,
    refetch: refetchIntegrations,
  } = useQuery<Integration[]>({
    queryKey: QUERY_KEYS.BUSINESS_INTEGRATIONS(activeBusinessId),
    queryFn: () =>
      bizApi(activeBusinessId!)
        .get(BIZ_API_PATHS.INTEGRATIONS.ROOT)
        .then((r) => (Array.isArray(r.data) ? r.data : []) as Integration[]),
    enabled: !!activeBusinessId,
  });

  const { data: business } = useQuery<Business>({
    queryKey: QUERY_KEYS.BUSINESS_PROFILE(activeBusinessId),
    queryFn: () =>
      bizApi(activeBusinessId!)
        .get<Business>(BIZ_API_PATHS.BUSINESS.ROOT)
        .then((r) => r.data),
    enabled: !!activeBusinessId,
  });

  useEffect(() => {
    prevIntegrationIdsRef.current = null;
    baselineBusinessIdRef.current = null;
    setLastRegistered(null);
  }, [activeBusinessId]);

  useEffect(() => {
    if (integrationsLoading) return;

    const currentIds = new Set(integrations.map((i) => i.id));
    const prev = prevIntegrationIdsRef.current;

    if (prev == null || baselineBusinessIdRef.current !== activeBusinessId) {
      prevIntegrationIdsRef.current = currentIds;
      baselineBusinessIdRef.current = activeBusinessId;
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
  }, [integrations, business?.id, integrationsLoading, activeBusinessId]);

  const disconnectMutation = useMutation({
    mutationFn: (integrationId: string) => {
      if (!activeBusinessId) return Promise.reject(new Error('No active business'));
      return bizApi(activeBusinessId).delete(BIZ_API_PATHS.INTEGRATIONS.BY_ID(integrationId));
    },
    onSuccess: () => {
      trackClick('disconnect_integration');
      qc.invalidateQueries({ queryKey: QUERY_KEYS.BUSINESS_INTEGRATIONS(activeBusinessId) });
      toast.success(tIntegrations('page.channelDisconnected'));
    },
    onError: () => toast.error(tIntegrations('page.disconnectFailed')),
  });

  const getIntegrationsForPlatform = (platformId: string): Integration[] =>
    integrations.filter((i) => i.platform === platformId);

  const handleConnect = async (platformId: string) => {
    trackClick('connect_integration', { platform: platformId });
    if (platformId === 'telegram' || platformId === 'vk' || platformId === 'yandex_business') {
      setActiveModalPlatform(platformId);
      return;
    }
    if (platformId === 'google_business') {
      try {
        if (!activeBusinessId) return;
        const authUrlPath = INTEGRATION_ENDPOINTS.google_business?.authUrl;
        if (!authUrlPath) return;
        const { data } = await bizApi(activeBusinessId).get<{ url: string }>(authUrlPath);
        window.location.href = data.url;
      } catch {
        toast.error(tIntegrations('page.googleAuthFailed'));
      }
    }
  };

  const closeActiveModal = () => {
    const platform = activeModalPlatform;
    setActiveModalPlatform(null);
    if (platform && MODAL_INVALIDATES_ON_CLOSE[platform]) {
      qc.invalidateQueries({ queryKey: QUERY_KEYS.BUSINESS_INTEGRATIONS(activeBusinessId) });
    }
  };

  const ActiveModal = activeModalPlatform ? MODAL_COMPONENTS[activeModalPlatform] : null;

  return (
    <>
      <PageHeader title={tIntegrations('title')} sub={tIntegrations('subtitle')} />

      <div className="px-4 pb-10 sm:px-12 sm:pb-16">
        <SectionHelp section="integrations" className="mb-8" />

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
        {integrationsError ? (
          <div className="mb-8">
            <ListLoadError onRetry={refetchIntegrations} />
          </div>
        ) : integrationsLoading ? (
          <div className="mb-8">
            <SkeletonChannels count={3} />
          </div>
        ) : integrations.length === 0 && canConnect ? (
          <div className="mb-8">
            <EmptyChannels
              onConnect={() => {
                const target = document.getElementById('integrations-platform-grid');
                if (target) {
                  target.scrollIntoView({ behavior: 'smooth', block: 'start' });
                }
              }}
            />
          </div>
        ) : integrations.length === 0 && !canConnect ? (
          <div className="mb-8">
            <InlineEmpty className="rounded-lg border border-line bg-paper-raised">
              {tIntegrations('page.viewerNoChannels')}
            </InlineEmpty>
          </div>
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
                description={tPlatformDesc(p.id)}
                integrations={platformIntegrations}
                onConnect={() => handleConnect(p.id)}
                onDisconnect={(integrationId) => disconnectMutation.mutate(integrationId)}
                canConnect={canConnect}
                canDisconnect={canDisconnect}
                isPreview={p.id === 'google_business'}
              />
            );
          })}
        </div>

        {activeBusinessId && integrations.length > 0 && (
          <IntegrationsSyncPanel businessId={activeBusinessId} integrations={integrations} />
        )}

        {comingSoonPlatforms.length > 0 && (
          <>
            <SectionLabel className="mt-12">{tIntegrations('page.comingSoon')}</SectionLabel>
            <div className="grid grid-cols-1 items-start gap-4 md:grid-cols-2 lg:grid-cols-3">
              {comingSoonPlatforms.map((p) => (
                <SoonCard
                  key={p.id}
                  label={p.fullLabel}
                  when={p.comingSoonWhen ?? tPlatforms('comingSoonFallback')}
                />
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

      {ActiveModal && <ActiveModal open={true} onClose={closeActiveModal} />}

      <VKCommunityPickerModal open={vkPickerOpen} onClose={() => setVkPickerOpen(false)} />

      <FirstActionWizard open={wizardOpen} onClose={() => setWizardOpen(false)} />
    </>
  );
}

function SectionLabel({ children, className }: { children: React.ReactNode; className?: string }) {
  return (
    <h2 className={`mb-4 mt-2 flex items-center gap-3 ${className ?? ''}`}>
      <MonoLabel>{children}</MonoLabel>
      <span aria-hidden className="h-px flex-1 bg-line-soft" />
    </h2>
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
