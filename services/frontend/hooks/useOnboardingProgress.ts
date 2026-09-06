'use client';

import { useQuery } from '@tanstack/react-query';
import { bizApi } from '@/lib/api/business-api';
import { API_PATHS } from '@/lib/constants/apiPaths';
import { BIZ_API_PATHS } from '@/lib/constants/bizApiPaths';
import { QUERY_KEYS } from '@/lib/constants/queryKeys';
import { useBusinessStore } from '@/lib/stores/business';
import { useBusinessList } from '@/lib/hooks/useBusinessList';
import type { AgentTask } from '@/types/task';
import { usePermission } from '@/lib/hooks/usePermission';
import { useMembers } from '@/lib/hooks/useMembers';
import { conversationsQueryKey } from '@/hooks/useConversations';
import type { Conversation } from '@/lib/conversations';
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

interface PersistedAnswer {
  role: string;
  content: string;
  status?: string;
  errorCode?: string;
  toolCalls?: unknown[];
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

  const actions = useQuery<AgentTask[]>({
    queryKey: [...QUERY_KEYS.BUSINESS_TASKS(activeBusinessId), 'first-action'],
    queryFn: async ({ signal }) => {
      const { data } = await bizApi(activeBusinessId!).get<AgentTask[] | { tasks?: AgentTask[] }>(
        BIZ_API_PATHS.TASKS.ROOT,
        { signal, params: { status: 'done', limit: 1 } }
      );
      return Array.isArray(data) ? data : (data?.tasks ?? []);
    },
    enabled: !!activeBusinessId,
    retry: false,
  });

  const hasSuccessfulTask =
    actions.isSuccess && (actions.data ?? []).some((action) => action.status === 'done');

  const textAnswer = useQuery({
    queryKey: [...conversationsQueryKey(activeBusinessId), 'first-action'],
    queryFn: async ({ signal }) => {
      const api = bizApi(activeBusinessId!);
      const limit = 100;
      for (let offset = 0; ; offset += limit) {
        const { data: conversations } = await api.get<Conversation[]>(
          BIZ_API_PATHS.CONVERSATIONS.ROOT,
          { signal, params: { limit, offset } }
        );
        for (const conversation of conversations) {
          const { data } = await api.get<PersistedAnswer[] | { messages: PersistedAnswer[] }>(
            BIZ_API_PATHS.CONVERSATIONS.MESSAGES(conversation.id),
            { signal }
          );
          const messages = Array.isArray(data) ? data : data.messages;
          if (
            messages.some(
              (message) =>
                message.role === 'assistant' &&
                message.status === 'complete' &&
                !message.errorCode &&
                message.content.trim() !== '' &&
                !message.toolCalls?.length
            )
          )
            return true;
        }
        if (conversations.length < limit) return false;
      }
    },
    enabled: !!activeBusinessId && (actions.isSuccess || actions.isError) && !hasSuccessfulTask,
    retry: false,
  });

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
    hasFirstAction: hasSuccessfulTask || (textAnswer.isSuccess && textAnswer.data),
    conversationsSettled:
      hasSuccessfulTask ||
      ((actions.isSuccess || actions.isError) && (textAnswer.isSuccess || textAnswer.isError)),
    showInvite: canInvite,
    hasTeammate: members.isSuccess && (members.data?.length ?? 0) > 1,
  });
}
