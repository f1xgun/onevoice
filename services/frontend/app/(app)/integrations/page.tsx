'use client';

import Link from 'next/link';
import { EmptyAction } from '@/components/states/EmptyAction';

import { useState, useEffect, useRef } from 'react';
import { useSearchParams } from 'next/navigation';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { useLocale, useTranslations } from 'next-intl';
import { toast } from 'sonner';

import { bizApi } from '@/lib/api/business-api';
import { API_PATHS } from '@/lib/constants/apiPaths';
import { BIZ_API_PATHS, INTEGRATION_ENDPOINTS } from '@/lib/constants/bizApiPaths';
import { QUERY_KEYS } from '@/lib/constants/queryKeys';
import { useBusinessStore } from '@/lib/stores/business';
import { trackClick } from '@/lib/telemetry';
import { ActionButton as Button } from '@/components/design-system/ActionButton';
import { SkeletonChannels } from '@/components/states';
import { InlineEmpty } from '@/components/states/InlineEmpty';
import { PlatformCard } from '@/components/integrations/PlatformCard';
import { TelegramConnectModal } from '@/components/integrations/TelegramConnectModal';
import { VKCommunityModal } from '@/components/integrations/VKCommunityModal';
import { VKCommunityPickerModal } from '@/components/integrations/VKCommunityPickerModal';
import { GoogleLocationModal } from '@/components/integrations/GoogleLocationModal';
import { YandexBusinessConnectModal } from '@/components/integrations/YandexBusinessConnectModal';
import { WhitelistWarningBanner } from '@/components/integrations/WhitelistWarningBanner';
import { FirstActionWizard } from '@/components/onboarding/FirstActionWizard';
import { ScopedBusinessLink } from '@/components/integrations/ScopedBusinessLink';
import { usePlatforms } from '@/lib/hooks/usePlatforms';
import { usePermission } from '@/lib/hooks/usePermission';
import { useIntegrationsDrift } from '@/lib/hooks/useIntegrationsDrift';
import { readConnectionHealth } from '@/lib/constants/integrationStatus';
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
  status: string;
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
  const tLaunch = useTranslations('launchV2');
  const [resumeBusiness, setResumeBusiness] = useState<string | null>(null);
  const tCommon = useTranslations('common');
  const tV2 = useTranslations('integrations.v2');
  const locale = useLocale();
  const tPlatformDesc = useTranslations('platforms.description');
  const searchParams = useSearchParams();
  const activeBusinessId = useBusinessStore((s) => s.activeBusinessId);
  const drift = useIntegrationsDrift(activeBusinessId);
  useEffect(() => {
    setResumeBusiness(sessionStorage.getItem('onevoice:launch-vk-business'));
  }, [activeBusinessId]);
  const activeBusiness = useRef(activeBusinessId);
  activeBusiness.current = activeBusinessId;
  const canConnect = usePermission('integrations.connect').allowed;
  const canDisconnect = usePermission('integrations.disconnect').allowed;
  const [activeModalPlatform, setActiveModalPlatform] = useState<ModalPlatform | null>(null);
  const [vkPickerOpen, setVkPickerOpen] = useState(false);
  const [lastRegistered, setLastRegistered] = useState<LastRegistered | null>(null);
  const [wizardOpen, setWizardOpen] = useState(false);
  const prevIntegrationIdsRef = useRef<Set<string> | null>(null);
  const baselineBusinessIdRef = useRef<string | null>(null);

  const {
    platforms,
    isSuccess: platformsReady,
    isPending: platformsLoading,
    isError: platformsError,
    refetch: refetchPlatforms,
  } = usePlatforms();
  const activePlatforms = platforms.filter(
    (p) => p.status === 'active' && !['2gis', 'avito'].includes(p.id)
  );
  const comingSoonPlatforms = platforms.filter(
    (p) => p.status === 'coming_soon' || ['2gis', 'avito'].includes(p.id)
  );

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

    const connect = searchParams.get('connect');
    if (
      canConnect &&
      platformsReady &&
      connect &&
      Object.hasOwn(MODAL_COMPONENTS, connect) &&
      platforms.some((platform) => platform.id === connect && platform.status === 'active') &&
      connect !== 'google_business'
    ) {
      setActiveModalPlatform(connect as ModalPlatform);
      window.history.replaceState({}, '', API_PATHS.INTEGRATIONS.ROOT);
    }

    const reconnect = searchParams.get('reconnect');
    if (canConnect && reconnect && reconnect in MODAL_COMPONENTS) {
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
  }, [searchParams, qc, activeBusinessId, tIntegrations, canConnect, platforms, platformsReady]);

  const {
    data: integrations = [],
    isPending: integrationsLoading,
    isError: integrationsError,
    refetch: refetchIntegrations,
  } = useQuery<Integration[]>({
    queryKey: QUERY_KEYS.BUSINESS_INTEGRATIONS(activeBusinessId),
    queryFn: () =>
      bizApi(activeBusinessId!)
        .get(BIZ_API_PATHS.INTEGRATIONS.ROOT)
        .then((r) => {
          if (!Array.isArray(r.data)) throw new Error('Invalid integration list response');
          return r.data as Integration[];
        }),
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
    if (integrationsLoading || integrationsError) return;

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
  }, [integrations, business?.id, integrationsLoading, integrationsError, activeBusinessId]);

  const disconnectMutation = useMutation({
    mutationFn: ({ integrationId, businessId }: { integrationId: string; businessId: string }) => {
      return bizApi(businessId).delete(BIZ_API_PATHS.INTEGRATIONS.BY_ID(integrationId));
    },
    onSuccess: (_, { businessId }) => {
      trackClick('disconnect_integration', undefined, businessId);
      qc.invalidateQueries({ queryKey: QUERY_KEYS.BUSINESS_INTEGRATIONS(businessId) });
      if (activeBusiness.current !== businessId) return;
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
      <ScopedBusinessLink />

      <div className="max-w-[816px] space-y-group px-gutter pb-10 pt-group md:px-gutter-desktop">
        <header>
          <h1 className="text-page-title">{tV2('title')}</h1>
          <p className="mt-1 text-[15px] leading-[22px] text-ink-soft">{tV2('subtitle')}</p>
        </header>

        {activeBusinessId && resumeBusiness === activeBusinessId && (
          <Link
            href={`/business/new?step=2&businessId=${encodeURIComponent(activeBusinessId)}`}
            className="inline-flex min-h-11 items-center text-brand underline"
            onClick={() => sessionStorage.removeItem('onevoice:launch-vk-business')}
          >
            {tLaunch('resume')}
          </Link>
        )}

        {lastRegistered && (
          <div className="mb-8">
            <WhitelistWarningBanner
              integrationId={lastRegistered.integrationId}
              businessId={lastRegistered.businessId}
              platform={lastRegistered.platform}
            />
          </div>
        )}

        {!activeBusinessId ? (
          <InlineEmpty
            action={<EmptyAction href="/business" action="chooseBusiness" />}
            className="mb-8 rounded-lg border border-line bg-paper-raised"
          >
            {tIntegrations('page.chooseOrganization')}
          </InlineEmpty>
        ) : integrationsError ? (
          <div
            role="status"
            className="mb-8 flex flex-col items-start gap-3 rounded-lg border border-line bg-paper-raised p-4 text-meta text-ink-soft"
          >
            <p>{tIntegrations('page.statusUnknown')}</p>
            <p>{tIntegrations('page.listUnavailable')}</p>
            <Button variant="secondary" size="sm" onClick={() => refetchIntegrations()}>
              {tCommon('retry')}
            </Button>
          </div>
        ) : integrationsLoading ? (
          <div className="mb-8">
            <SkeletonChannels count={3} />
          </div>
        ) : integrations.length === 0 && !canConnect ? (
          <div className="mb-8">
            <InlineEmpty
              action={<EmptyAction href="/getting-started" action="gettingStarted" />}
              className="rounded-lg border border-line bg-paper-raised"
            >
              {tIntegrations('page.viewerNoChannels')}
            </InlineEmpty>
          </div>
        ) : null}
        {activeBusinessId && !integrationsError && !integrationsLoading && platformsReady && (
          <div
            id="integrations-platform-grid"
            className="!mt-title-gap rounded-block bg-block p-1 md:p-2"
          >
            {activePlatforms.map((p) => {
              const platformIntegrations = getIntegrationsForPlatform(p.id);
              return (
                <PlatformCard
                  key={p.id}
                  platform={p.id}
                  label={tV2.has(`platforms.${p.id}`) ? tV2(`platforms.${p.id}`) : p.fullLabel}
                  description={tPlatformDesc(p.id)}
                  integrations={platformIntegrations}
                  onConnect={() => handleConnect(p.id)}
                  onDisconnect={(integrationId) =>
                    activeBusinessId &&
                    disconnectMutation.mutate({ integrationId, businessId: activeBusinessId })
                  }
                  canConnect={canConnect}
                  canDisconnect={canDisconnect}
                  isPreview={p.id === 'google_business'}
                  checkedAt={
                    platformIntegrations
                      .map(
                        (integration) =>
                          readConnectionHealth(integration.metadata)?.checked_at ||
                          drift.data?.find(
                            (row) =>
                              row.platform === integration.platform &&
                              row.externalId === integration.externalId &&
                              !row.checkFailed
                          )?.lastCheckedAt
                      )
                      .filter(
                        (value): value is string => !!value && !Number.isNaN(Date.parse(value))
                      )
                      .sort()[0]
                  }
                />
              );
            })}
          </div>
        )}

        {platformsLoading && <SkeletonChannels count={3} />}
        {platformsError && (
          <div role="alert" className="space-y-3 rounded-block bg-block p-block">
            <p>{tV2('registryError')}</p>
            <Button variant="secondary" onClick={() => void refetchPlatforms()}>
              {tCommon('retry')}
            </Button>
          </div>
        )}
        {platformsReady &&
          platforms.filter((p) => p.status === 'oauth_not_configured').length > 0 && (
            <section>
              <h2 className="mb-block-gap text-group-title">{tV2('unavailable')}</h2>
              <div className="rounded-block bg-block p-block">
                {platforms
                  .filter((p) => p.status === 'oauth_not_configured')
                  .map((p) => (
                    <p key={p.id} className="text-meta text-ink-soft">
                      {p.fullLabel} — {tV2('notConfigured')}
                    </p>
                  ))}
              </div>
            </section>
          )}
        {platformsReady && comingSoonPlatforms.length > 0 && (
          <section>
            <h2 className="mb-block-gap text-group-title">{tIntegrations('page.comingSoon')}</h2>
            <p className="text-reading text-ink-soft">
              {tV2('comingSoonDescription', {
                platforms: new Intl.ListFormat(locale, {
                  style: 'long',
                  type: 'conjunction',
                }).format(
                  [...comingSoonPlatforms]
                    .sort(
                      (a, b) =>
                        ['2gis', 'avito', 'google_business', 'whatsapp'].indexOf(a.id) -
                        ['2gis', 'avito', 'google_business', 'whatsapp'].indexOf(b.id)
                    )
                    .map((p) => (p.id === 'google_business' ? tV2('googleName') : p.fullLabel))
                ),
              })}
            </p>
            <a
              href="mailto:hello@onevoice.app"
              className="inline-flex min-h-tap items-center text-reading text-brand underline"
            >
              {tV2('requestPlatform')}
            </a>
          </section>
        )}
        <p className="text-meta text-ink-soft">{tV2('checkSchedule')}</p>
      </div>

      {ActiveModal && <ActiveModal open={true} onClose={closeActiveModal} />}

      <VKCommunityPickerModal open={vkPickerOpen} onClose={() => setVkPickerOpen(false)} />

      <FirstActionWizard open={wizardOpen} onClose={() => setWizardOpen(false)} />
    </>
  );
}
