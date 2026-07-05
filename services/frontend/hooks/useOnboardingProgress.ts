'use client';

import { useQuery } from '@tanstack/react-query';
import { bizApi } from '@/lib/api/business-api';
import { API_PATHS } from '@/lib/constants/apiPaths';
import { BIZ_API_PATHS } from '@/lib/constants/bizApiPaths';
import { QUERY_KEYS } from '@/lib/constants/queryKeys';
import { useBusinessStore } from '@/lib/stores/business';
import { useBusinessList } from '@/lib/hooks/useBusinessList';
import { useConversationsQuery } from '@/hooks/useConversations';
import { usePermission } from '@/lib/hooks/usePermission';
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

export interface OnboardingProgress {
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

// useOnboardingProgress composes four queries the app already runs — the
// business list, the business-integrations list, the business profile, and the
// conversations list — into a derived checklist. It issues NO new backend
// endpoint: the integrations/profile/conversations queries reuse the exact
// QUERY_KEYS other surfaces mount, so they share warm caches. The optional
// invite step reads the members list, fetched only for actors who can invite.
export function useOnboardingProgress(): OnboardingProgress {
  const activeBusinessId = useBusinessStore((s) => s.activeBusinessId);

  const businessList = useBusinessList();

  const integrations = useQuery<{ status: string }[]>({
    queryKey: QUERY_KEYS.BUSINESS_INTEGRATIONS(activeBusinessId),
    queryFn: () =>
      bizApi(activeBusinessId!)
        .get(BIZ_API_PATHS.INTEGRATIONS.ROOT)
        .then((r) => (Array.isArray(r.data) ? r.data : []) as { status: string }[]),
    enabled: !!activeBusinessId,
    retry: false,
    placeholderData: [],
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

  const conversations = useConversationsQuery();

  const canInvite = usePermission('members.invite').allowed;
  const members = useMembers(canInvite ? activeBusinessId : null);

  const description = profile.data?.description;

  return deriveOnboarding({
    hasBusiness: businessList.isSuccess && (businessList.data?.length ?? 0) >= 1,
    businessSettled: businessList.isSuccess || businessList.isError,
    hasActiveIntegration:
      integrations.isSuccess && (integrations.data ?? []).some((i) => i.status === 'active'),
    integrationsSettled: integrations.isSuccess || integrations.isError,
    hasDescription:
      profile.isSuccess && typeof description === 'string' && description.trim() !== '',
    profileSettled: profile.isSuccess || profile.isError,
    hasFirstAction:
      conversations.isSuccess && (conversations.data ?? []).some((c) => !!c.lastMessageAt),
    conversationsSettled: conversations.isSuccess || conversations.isError,
    showInvite: canInvite,
    hasTeammate: members.isSuccess && (members.data?.length ?? 0) > 1,
  });
}
