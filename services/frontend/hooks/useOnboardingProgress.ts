'use client';

import { useQuery } from '@tanstack/react-query';
import { bizApi } from '@/lib/api/business-api';
import { API_PATHS } from '@/lib/constants/apiPaths';
import { BIZ_API_PATHS } from '@/lib/constants/bizApiPaths';
import { QUERY_KEYS } from '@/lib/constants/queryKeys';
import { useBusinessStore } from '@/lib/stores/business';
import { useBusinessList } from '@/lib/hooks/useBusinessList';
import { usePermission } from '@/lib/hooks/usePermission';
import { usePlatforms } from '@/lib/hooks/usePlatforms';
import {
  channelConnectionState,
  type ChannelConnectionState,
} from '@/lib/constants/integrationStatus';
import { useMembers } from '@/lib/hooks/useMembers';
import type { Business } from '@/types/business';

export type OnboardingStepId =
  | 'createOrg'
  | 'connectChannel'
  | 'describeOrg'
  | 'firstAction'
  | 'inviteTeam';

export interface OnboardingStep {
  id: OnboardingStepId;
  href: string;
  done: boolean;
  loading: boolean;
  /** Gating steps count toward progress + allDone; non-gating are suggestions. */
  gating: boolean;
}

export interface OnboardingChannel {
  platform: string;
  label: string;
  state: ChannelConnectionState | 'loading';
  href: string;
  canConnect: boolean;
}

export interface OnboardingProgress {
  channels?: OnboardingChannel[];
  steps: OnboardingStep[];
  completedCount: number;
  total: number;
  allDone: boolean;
  loaded: boolean;
}

// Resolved query signals fed into the pure derivation. Every `has*` flag is
// already isSuccess-gated by the caller so a transient fetch error never reads
// an empty default as a real answer (the isPlaceholderData trap); the `*Settled`
// flags mark whether the query has a definitive answer (success OR error), which
// drives the per-step loading spinner without stranding a step on an error.
export interface OnboardingSignals {
  hasBusiness: boolean;
  businessSettled: boolean;
  hasActiveIntegration: boolean;
  integrationsSettled: boolean;
  hasDescription: boolean;
  profileSettled: boolean;
  hasFirstAction: boolean;
  conversationsSettled: boolean;
  showInvite: boolean;
  hasTeammate: boolean;
}

const ROUTES = {
  createOrg: API_PATHS.BUSINESS.ROOT,
  connectChannel: API_PATHS.INTEGRATIONS.ROOT,
  describeOrg: API_PATHS.BUSINESS.ROOT,
  firstAction: '/chat',
  inviteTeam: '/settings/team',
} as const;

export function deriveOnboarding(s: OnboardingSignals): OnboardingProgress {
  const steps: OnboardingStep[] = [
    {
      id: 'createOrg',
      href: ROUTES.createOrg,
      done: s.hasBusiness,
      loading: !s.businessSettled,
      gating: true,
    },
    {
      id: 'connectChannel',
      href: ROUTES.connectChannel,
      done: s.hasActiveIntegration,
      loading: !s.integrationsSettled,
      gating: true,
    },
    {
      id: 'describeOrg',
      href: ROUTES.describeOrg,
      done: s.hasDescription,
      loading: !s.profileSettled,
      gating: true,
    },
    {
      id: 'firstAction',
      href: ROUTES.firstAction,
      done: s.hasFirstAction,
      loading: !s.conversationsSettled,
      gating: true,
    },
  ];
  if (s.showInvite) {
    steps.push({
      id: 'inviteTeam',
      href: ROUTES.inviteTeam,
      done: s.hasTeammate,
      loading: false,
      gating: false,
    });
  }
  const gating = steps.filter((step) => step.gating);
  const completedCount = gating.filter((step) => step.done).length;
  const total = gating.length;
  const loaded = gating.every((step) => !step.loading);
  const allDone = loaded && completedCount === total;
  return { steps, completedCount, total, allDone, loaded };
}

export function useOnboardingProgress(): OnboardingProgress {
  const activeBusinessId = useBusinessStore((s) => s.activeBusinessId);

  const businessList = useBusinessList();
  const registry = usePlatforms();
  const canConnect = usePermission('integrations.connect').allowed;

  const integrations = useQuery<
    { platform: string; status: string; metadata?: Record<string, unknown> }[]
  >({
    queryKey: QUERY_KEYS.BUSINESS_INTEGRATIONS(activeBusinessId),
    queryFn: () =>
      bizApi(activeBusinessId!)
        .get(BIZ_API_PATHS.INTEGRATIONS.ROOT)
        .then((r) => {
          if (!Array.isArray(r.data)) throw new Error('Invalid integration list response');
          return r.data;
        }),
    enabled: !!activeBusinessId,
    retry: false,
  });

  const profile = useQuery<Business>({
    queryKey: QUERY_KEYS.BUSINESS_PROFILE(activeBusinessId),
    queryFn: () =>
      bizApi(activeBusinessId!)
        .get<Business>(BIZ_API_PATHS.BUSINESS.ROOT)
        .then((r) => r.data),
    enabled: !!activeBusinessId,
    retry: false,
  });

  const canInvite = usePermission('members.invite').allowed;
  const members = useMembers(canInvite ? activeBusinessId : null);

  const description = profile.data?.description;

  const progress = deriveOnboarding({
    hasBusiness: businessList.isSuccess && (businessList.data?.length ?? 0) >= 1,
    businessSettled: businessList.isSuccess || businessList.isError,
    hasActiveIntegration:
      integrations.isSuccess && (integrations.data ?? []).some((i) => i.status === 'active'),
    integrationsSettled: integrations.isSuccess || integrations.isError,
    hasDescription:
      profile.isSuccess && typeof description === 'string' && description.trim() !== '',
    profileSettled: profile.isSuccess || profile.isError,
    hasFirstAction: profile.isSuccess && profile.data?.hasFirstSuccessfulAction === true,
    conversationsSettled: profile.isSuccess || profile.isError,
    showInvite: canInvite,
    hasTeammate: members.isSuccess && (members.data?.length ?? 0) > 1,
  });
  const channels: OnboardingChannel[] = registry.isSuccess
    ? registry.platforms
        .filter((platform) => platform.status === 'active' && platform.id !== 'google_business')
        .map((platform) => {
          const state = integrations.isPending
            ? 'loading'
            : integrations.isSuccess
              ? channelConnectionState(
                  integrations.data.filter((row) => row.platform === platform.id)
                )
              : 'unknown';
          return {
            platform: platform.id,
            label: platform.fullLabel,
            state,
            href: `${API_PATHS.INTEGRATIONS.ROOT}?${state === 'error' ? 'reconnect' : 'connect'}=${platform.id}`,
            canConnect:
              !!activeBusinessId && canConnect && (state === 'disconnected' || state === 'error'),
          };
        })
    : [];
  return { ...progress, channels };
}
