'use client';

import { useState, useMemo, useCallback } from 'react';
import { Send, Square } from 'lucide-react';
import { useQuery } from '@tanstack/react-query';
import { useTranslations } from 'next-intl';
import { GettingStartedChecklist } from '@/components/onboarding/GettingStartedChecklist';
import { FirstActionWizard } from '@/components/onboarding/FirstActionWizard';
import { GuidedCompose } from '@/components/onboarding/GuidedCompose';
import { SectionHelp } from '@/components/onboarding/SectionHelp';
import { ActionButton as Button } from '@/components/design-system/ActionButton';
import { AppTextarea as Input } from '@/components/design-system/AppInput';
import { SkeletonChat } from '@/components/states';
import { ListLoadError } from '@/components/lists/ListLoadError';
import { useConversationFlow } from '@/hooks/useConversationFlow';
import { useProjectsQuery } from '@/hooks/useProjects';
import { useDefaultQuickActions } from '@/lib/quick-actions';
import { bizApi } from '@/lib/api/business-api';
import { BIZ_API_PATHS } from '@/lib/constants/bizApiPaths';
import { QUERY_KEYS } from '@/lib/constants/queryKeys';
import { useBusinessStore } from '@/lib/stores/business';
import { usePermission } from '@/lib/hooks/usePermission';
import { PermissionLoadError } from '@/components/permission/PermissionLoadError';
import { trackEvent } from '@/lib/telemetry';
import type { Conversation } from '@/lib/conversations';
import { useChatComposer } from '@/hooks/useChatComposer';
import { TemplateChips } from '@/components/templates/TemplateChips';
import { ChatHeader } from './ChatHeader';
import { MessageBubble } from './MessageBubble';
import { ChatPhotoInput } from './ChatPhotoInput';
import Link from 'next/link';
import { ConnectChannelHint, shouldPromptConnectChannel } from './ConnectChannelHint';
import { ToolApprovalCard } from './ToolApprovalCard';
import { ExpiredApprovalBanner } from './ExpiredApprovalBanner';
import { ProcessingApprovalBanner } from './ProcessingApprovalBanner';
import { IntegrationTokenInvalidBanner } from './IntegrationTokenInvalidBanner';

interface ChatWindowProps {
  conversationId: string;
  initialPrompt?: string;
  onConversationDeleted?: () => void;
}

export function ChatWindow({
  conversationId,
  onConversationDeleted,
  initialPrompt,
}: ChatWindowProps) {
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
    dismissApproval,
    resolveApproval,
  } = useConversationFlow({ conversationId });
  const [wizardOpen, setWizardOpen] = useState(false);
  const [photoUploading, setPhotoUploading] = useState(false);
  const activeBusinessId = useBusinessStore((s) => s.activeBusinessId);

  const openWizard = useCallback(() => {
    trackEvent('activation', 'open_wizard', { metadata: { source: 'getting_started_checklist' } });
    setWizardOpen(true);
  }, []);
  const closeWizard = useCallback(() => setWizardOpen(false), []);

  const sendPerm = usePermission('content.create');
  const canSend = sendPerm.allowed;
  const composerDisabled =
    isLoading ||
    isStreaming ||
    awaitingTurn ||
    pendingApproval !== null ||
    !canSend ||
    loadError ||
    photoUploading;

  const {
    composerId,
    attachment,
    setAttachment,
    register,
    input,
    bottomRef,
    handleSend,
    handleScroll,
    handleKeyDown,
    prefill,
  } = useChatComposer({ messages, disabled: composerDisabled, sendMessage, initialPrompt });

  const { data: conversation } = useQuery<Conversation>({
    queryKey: ['businesses', activeBusinessId, 'conversations', conversationId],
    queryFn: ({ signal }) =>
      bizApi(activeBusinessId!)
        .get<Conversation>(BIZ_API_PATHS.CONVERSATIONS.BY_ID(conversationId), { signal })
        .then((r) => r.data),
    enabled: !!conversationId && !!activeBusinessId,
  });

  const { data: projects } = useProjectsQuery();

  const currentProject = useMemo(() => {
    if (!conversation?.projectId || !projects) return null;
    return projects.find((p) => p.id === conversation.projectId) ?? null;
  }, [conversation?.projectId, projects]);

  const defaultQuickActions = useDefaultQuickActions();
  const quickActions =
    currentProject?.quickActions && currentProject.quickActions.length > 0
      ? currentProject.quickActions
      : defaultQuickActions;
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

  return (
    <div data-ov-motion className="flex h-full min-h-0 min-w-0 flex-col">
      <Link href="/chat" className="px-gutter pt-4 text-sm text-brand md:px-gutter-desktop">
        {tChat('instructionsBack')}
      </Link>
      <FirstActionWizard open={wizardOpen} onClose={closeWizard} />
      {!showEmptyState && (
        <ChatHeader
          conversationId={conversationId}
          onConversationDeleted={onConversationDeleted}
          menuTitle={conversation?.title}
          menuTitleStatus={conversation?.titleStatus}
          menuProjectId={conversation?.projectId ?? null}
        />
      )}

      <div
        onScroll={handleScroll}
        className="min-h-0 flex-1 scroll-pb-28 scroll-pt-20 overflow-y-auto bg-background p-gutter md:scroll-py-8 md:p-gutter-desktop"
      >
        {isLoading ? (
          <SkeletonChat className="bg-transparent p-0" />
        ) : loadError ? (
          <div className="flex min-h-full items-center justify-center py-6">
            <ListLoadError onRetry={reload} />
          </div>
        ) : messages.length === 0 ? (
          <div className="flex min-h-full flex-col items-center justify-center gap-4 py-6">
            <GettingStartedChecklist className="w-full max-w-lg" onOpenWizard={openWizard} />
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
                <TemplateChips
                  businessId={activeBusinessId}
                  conversationKey={conversationId}
                  disabled={composerDisabled}
                  currentInput={input}
                  onPrefill={prefill}
                />
                <GuidedCompose
                  onCompose={(instruction, selectedPlatforms) =>
                    sendMessage(instruction, undefined, { selectedPlatforms })
                  }
                  disabled={composerDisabled}
                />
              </>
            )}
            <SectionHelp section="chat" className="w-full max-w-md" />
          </div>
        ) : (
          messages.map((msg) => <MessageBubble key={msg.id} message={msg} />)
        )}
        {(pendingApproval?.status === 'expired' || pendingApproval?.status === 'unavailable') && (
          <ExpiredApprovalBanner status={pendingApproval.status} onDismiss={dismissApproval} />
        )}
        {pendingApproval?.status === 'resolving' && <ProcessingApprovalBanner />}
        {tokenInvalidCall && (
          <IntegrationTokenInvalidBanner platform={tokenInvalidCall.name.split('__')[0] ?? ''} />
        )}
        {pendingApproval?.status === 'pending' && (
          <div className="bg-background p-gutter md:px-gutter-desktop">
            <ToolApprovalCard batch={pendingApproval} onSubmit={resolveApproval} />
          </div>
        )}

        <div ref={bottomRef} />
      </div>

      <div className="bg-background p-gutter md:px-gutter-desktop">
        {!showEmptyState && (
          <div className="mb-3">
            <TemplateChips
              businessId={activeBusinessId}
              conversationKey={conversationId}
              disabled={composerDisabled}
              currentInput={input}
              onPrefill={prefill}
            />
          </div>
        )}
        <label htmlFor={composerId} className="mb-2 block text-meta font-medium">
          {tChat('messagePlaceholder')}
        </label>
        <div className="flex gap-2 rounded-block bg-paper-raised p-2 transition-colors focus-within:border-brand">
          <Input
            id={composerId}
            {...register('message')}
            rows={3}
            onKeyDown={handleKeyDown}
            placeholder={tChat('messagePlaceholder')}
            aria-label={tChat('messagePlaceholder')}
            disabled={composerDisabled}
            className="flex-1 border-0 bg-paper text-ink shadow-none focus:border-0 focus:ring-0"
          />
          <ChatPhotoInput
            disabled={composerDisabled}
            onChange={setAttachment}
            onUploadingChange={setPhotoUploading}
          />
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
        {attachment && (
          <p className="mt-2 flex items-center gap-3 text-sm text-ink-soft">
            {attachment.name}
            <button
              type="button"
              disabled={composerDisabled}
              onClick={() => setAttachment(undefined)}
            >
              {tChat('removePhoto')}
            </button>
          </p>
        )}
        <p className="mt-2 text-meta text-ink-soft">{tChat('sendHint')}</p>
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
