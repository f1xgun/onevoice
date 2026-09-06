'use client';

import { useRef, useEffect, useState, useMemo, useCallback } from 'react';
import { Send, Square } from 'lucide-react';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { useTranslations } from 'next-intl';
import { toast } from 'sonner';
import { ChatHeader } from './ChatHeader';
import { MessageBubble } from './MessageBubble';
import { ProjectChip } from './ProjectChip';
import { ProjectPickerChip } from './ProjectPickerChip';
import { ConnectChannelHint, shouldPromptConnectChannel } from './ConnectChannelHint';
import { GettingStartedChecklist } from '@/components/onboarding/GettingStartedChecklist';
import { FirstActionWizard } from '@/components/onboarding/FirstActionWizard';
import { GuidedCompose } from '@/components/onboarding/GuidedCompose';
import { SectionHelp } from '@/components/onboarding/SectionHelp';
import { ToolApprovalCard } from './ToolApprovalCard';
import { ExpiredApprovalBanner } from './ExpiredApprovalBanner';
import { ProcessingApprovalBanner } from './ProcessingApprovalBanner';
import { IntegrationTokenInvalidBanner } from './IntegrationTokenInvalidBanner';
import { ActionButton as Button } from '@/components/design-system/ActionButton';
import { AppInput as Input } from '@/components/design-system/AppInput';
import { SkeletonChat } from '@/components/states';
import { ListLoadError } from '@/components/lists/ListLoadError';
import { useConversationFlow } from '@/hooks/useConversationFlow';
import { useProjectsQuery } from '@/hooks/useProjects';
import { useMoveConversation, conversationsQueryKey } from '@/hooks/useConversations';
import { useDefaultQuickActions } from '@/lib/quick-actions';
import { bizApi } from '@/lib/api/business-api';
import { BIZ_API_PATHS } from '@/lib/constants/bizApiPaths';
import { QUERY_KEYS } from '@/lib/constants/queryKeys';
import { useBusinessStore } from '@/lib/stores/business';
import { usePermission } from '@/lib/hooks/usePermission';
import { PermissionLoadError } from '@/components/permission/PermissionLoadError';
import { trackEvent } from '@/lib/telemetry';
import type { Conversation } from '@/lib/conversations';

interface ChatWindowProps {
  conversationId: string;
  // Forwarded to ChatHeader → ChatRowMenu so the chat owner (chat/[id]/page)
  // can redirect after delete without ChatWindow/ChatHeader pulling the
  // Next.js router into their isolated test fixtures.
  onConversationDeleted?: () => void;
}

export function ChatWindow({ conversationId, onConversationDeleted }: ChatWindowProps) {
  const tChat = useTranslations('chat.window');
  const {
    messages,
    isLoading,
    loadError,
    reload,
    isStreaming,
    awaitingTurn,
    sendMessage,
    stop,
    pendingApproval,
    resolveApproval,
  } = useConversationFlow({ conversationId });
  const [input, setInput] = useState('');
  const [wizardOpen, setWizardOpen] = useState(false);
  const bottomRef = useRef<HTMLDivElement>(null);
  const qc = useQueryClient();
  const activeBusinessId = useBusinessStore((s) => s.activeBusinessId);

  const openWizard = useCallback(() => {
    trackEvent('activation', 'open_wizard', { metadata: { source: 'getting_started_checklist' } });
    setWizardOpen(true);
  }, []);
  const closeWizard = useCallback(() => setWizardOpen(false), []);

  const sendPerm = usePermission('content.create');
  const canSend = sendPerm.allowed;

  // awaitingTurn: a prior turn is still generating server-side (we reloaded
  // mid-turn). Block new sends — the backend would reject with
  // turn_already_in_progress anyway.
  const composerDisabled =
    isStreaming || awaitingTurn || pendingApproval !== null || !canSend || loadError;

  const { data: conversation } = useQuery<Conversation>({
    queryKey: ['businesses', activeBusinessId, 'conversations', conversationId],
    queryFn: () =>
      bizApi(activeBusinessId!)
        .get<Conversation>(BIZ_API_PATHS.CONVERSATIONS.BY_ID(conversationId))
        .then((r) => r.data),
    enabled: !!conversationId && !!activeBusinessId,
  });

  const { data: projects } = useProjectsQuery();
  const move = useMoveConversation();

  const currentProject = useMemo(() => {
    if (!conversation?.projectId || !projects) return null;
    return projects.find((p) => p.id === conversation.projectId) ?? null;
  }, [conversation?.projectId, projects]);

  const defaultQuickActions = useDefaultQuickActions();
  const quickActions =
    currentProject?.quickActions && currentProject.quickActions.length > 0
      ? currentProject.quickActions
      : defaultQuickActions;

  // Shares the NavRail integrations query (same key → warm cache, no extra
  // fetch). With no connected channel the quick-action chips fire a tool-less
  // LLM turn that silently no-ops, so we swap them for a connect nudge — but
  // only on a SUCCESSFUL load, so a user with channels never sees a flash of
  // the nudge while loading, and a transient /integrations error fails open to
  // the chips rather than a possibly-wrong dead-end (isSuccess, not
  // !isPlaceholderData: the latter also flips false on an error with no data).
  const { data: integrations = [], isSuccess: integrationsLoaded } = useQuery<
    { platform: string; status: string }[]
  >({
    queryKey: QUERY_KEYS.BUSINESS_INTEGRATIONS(activeBusinessId),
    queryFn: () =>
      bizApi(activeBusinessId!)
        .get(BIZ_API_PATHS.INTEGRATIONS.ROOT)
        .then(
          (r) => (Array.isArray(r.data) ? r.data : []) as { platform: string; status: string }[]
        ),
    enabled: !!activeBusinessId,
    retry: false,
    placeholderData: [],
  });
  const showConnectHint = shouldPromptConnectChannel(integrations, integrationsLoaded);

  const showEmptyState = messages.length === 0 && !isLoading && !loadError;

  const tokenInvalidCall = useMemo(() => {
    for (let i = messages.length - 1; i >= 0; i--) {
      const m = messages[i];
      if (m.role !== 'assistant') continue;
      if (!m.toolCalls) {
        return null;
      }
      const hit = m.toolCalls.find((tc) => tc.code === 'integration_token_invalid');
      return hit ?? null;
    }
    return null;
  }, [messages]);

  useEffect(() => {
    bottomRef.current?.scrollIntoView({ behavior: 'smooth' });
  }, [messages]);

  const handleSend = async () => {
    const text = input.trim();
    if (!text || composerDisabled) return;
    setInput('');
    await sendMessage(text);
  };

  const handlePickerChange = (projectId: string | null) => {
    if (!conversation) return;
    if ((conversation.projectId ?? null) === projectId) return;
    move.mutate(
      {
        id: conversationId,
        projectId,
        previousProjectId: conversation.projectId ?? null,
      },
      {
        onSuccess: () => {
          void qc.invalidateQueries({
            queryKey: ['businesses', activeBusinessId, 'conversations', conversationId],
          });
          void qc.invalidateQueries({ queryKey: conversationsQueryKey(activeBusinessId) });
        },
        onError: () => {
          toast.error(tChat('moveError'));
        },
      }
    );
  };

  return (
    <div className="flex h-full flex-col">
      <FirstActionWizard open={wizardOpen} onClose={closeWizard} />

      {/* Chat header — (USER OVERRIDE) Landmine 1:
          isolated, memoized subtree subscribed via useQuery `select` to a
          primitive string. Rendered as a SIBLING of the message list and
          composer below so title changes do not destroy composer focus or
          scroll position. */}
      {!showEmptyState && (
        <ChatHeader
          conversationId={conversationId}
          onConversationDeleted={onConversationDeleted}
          menuTitle={conversation?.title}
          menuTitleStatus={conversation?.titleStatus}
          menuProjectId={conversation?.projectId ?? null}
          rightSlot={
            <ProjectChip
              projectId={currentProject?.id ?? null}
              projectName={currentProject?.name}
            />
          }
        />
      )}

      {/* Messages — paper-well backdrop matches mock-ai-chat.jsx (line 146). */}
      <div className="flex-1 scroll-pb-28 scroll-pt-20 overflow-y-auto bg-paper-well px-4 py-4 sm:px-6 sm:py-6 md:scroll-py-8">
        {isLoading ? (
          <SkeletonChat className="bg-transparent p-0" />
        ) : loadError ? (
          <div className="flex min-h-full items-center justify-center py-6">
            <ListLoadError onRetry={reload} />
          </div>
        ) : messages.length === 0 ? (
          <div className="flex min-h-full flex-col items-center justify-center gap-4 py-6">
            {/* Self-contained activation checklist. Kept independent of the
                composer/chips so the surface stays shared-safe with the
                parallel first-action wizard, which mounts via onOpenWizard. */}
            <GettingStartedChecklist className="w-full max-w-lg" onOpenWizard={openWizard} />
            <ProjectPickerChip
              value={conversation?.projectId ?? null}
              onChange={handlePickerChange}
            />
            <p className="text-lg text-ink-soft">{tChat('helpPrompt')}</p>
            {showConnectHint ? (
              <ConnectChannelHint />
            ) : (
              <>
                <div className="flex flex-wrap justify-center gap-2">
                  {quickActions.map((action) => (
                    <button
                      key={action}
                      type="button"
                      onClick={() => sendMessage(action)}
                      disabled={composerDisabled}
                      className="rounded-full border border-line bg-paper-raised px-4 py-2 text-sm text-ink-mid transition-colors hover:bg-paper-sunken hover:text-ink disabled:cursor-not-allowed disabled:opacity-50"
                    >
                      {action}
                    </button>
                  ))}
                </div>
                {/* Guided compose seeds a templated instruction into the SAME
                    sendMessage path the chips use; the model drafts and its
                    publish tool call surfaces the existing ToolApprovalCard. */}
                <GuidedCompose onCompose={sendMessage} disabled={composerDisabled} />
              </>
            )}
            <SectionHelp section="chat" className="w-full max-w-md" />
          </div>
        ) : (
          messages.map((msg) => <MessageBubble key={msg.id} message={msg} />)
        )}
        <div ref={bottomRef} />
      </div>

      {/* Expired approval banner — sits above the card; owned by. */}
      {pendingApproval?.status === 'expired' && <ExpiredApprovalBanner />}

      {/* Processing banner — the batch was approved and the resume is running
          server-side (resolving). Replaces the actionable card so a reload
          mid-resume cannot re-submit (which would 409 already_resolved). */}
      {pendingApproval?.status === 'resolving' && <ProcessingApprovalBanner />}

      {/* Integration-token-invalid banner — surfaces above the composer when
          the newest assistant turn's tool call failed with the typed code. */}
      {tokenInvalidCall && (
        <IntegrationTokenInvalidBanner platform={tokenInvalidCall.name.split('__')[0] ?? ''} />
      )}

      {/* Inline approval card — renders only when a pending batch exists. */}
      {pendingApproval?.status === 'pending' && (
        <div className="border-t border-line bg-paper px-3 py-3 sm:px-4 sm:py-4">
          <ToolApprovalCard batch={pendingApproval} onSubmit={resolveApproval} />
        </div>
      )}

      {/* Composer — Linen rebuild per mock-ai-chat.jsx Composer (lines
          308–325): an outer paper section that hosts a paper-sunken
          inner card. Input handles the ochre focus ring; the send button
          stays graphite (variant="primary") because ochre is reserved
          for the single moment of emphasis in this surface (the inline
          ApprovalCard). The keep-disabled-while-streaming-or-pending
          contract is unchanged. */}
      <div className="border-t border-line bg-paper px-3 py-3 sm:px-4 sm:py-4">
        <div className="flex gap-2 rounded-md border border-line bg-paper-sunken p-2 transition-colors focus-within:border-brand">
          <Input
            value={input}
            onChange={(e) => setInput(e.target.value)}
            onKeyDown={(e) => e.key === 'Enter' && !e.shiftKey && void handleSend()}
            placeholder={tChat('messagePlaceholder')}
            aria-label={tChat('messagePlaceholder')}
            disabled={composerDisabled}
            className="flex-1 border-0 bg-paper text-ink shadow-none focus:border-0 focus:ring-0"
          />
          {/* TODO(design): slash-commands chip rail (`/ Команды`) per
              mock — backend command registry not in scope for v1.3, so
              the placeholder is deferred. */}
          {isStreaming ? (
            <Button variant="outline" size="md" onClick={stop} aria-label={tChat('stopAria')}>
              <Square size={16} />
            </Button>
          ) : (
            <Button
              variant="primary"
              size="md"
              onClick={handleSend}
              disabled={composerDisabled || !input.trim()}
              aria-label={tChat('sendAria')}
            >
              <Send size={16} />
            </Button>
          )}
        </div>
        {sendPerm.isError ? (
          <div className="mt-2">
            <PermissionLoadError onRetry={sendPerm.refetch} />
          </div>
        ) : (
          !canSend && <p className="mt-2 text-xs text-ink-soft">{tChat('readOnlyHint')}</p>
        )}
      </div>
    </div>
  );
}
