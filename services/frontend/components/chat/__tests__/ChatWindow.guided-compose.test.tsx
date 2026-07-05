import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type { ReactNode } from 'react';

import { ChatWindow } from '../ChatWindow';
import { useConversationFlow } from '@/hooks/useConversationFlow';
import { useBusinessStore } from '@/lib/stores/business';
import { singleCallBatch } from '@/test-utils/pending-approval-fixtures';
import type { PendingApproval } from '@/types/chat';

vi.mock('sonner', () => ({
  toast: { success: vi.fn(), error: vi.fn() },
}));

vi.mock('@/lib/telemetry', () => ({
  trackEvent: vi.fn(),
}));

vi.mock('@/lib/hooks/usePermission', () => ({
  usePermission: () => ({ allowed: true, isLoading: false }),
}));

// Inject a spy sendMessage so the test asserts the exact composed string the
// guided-compose picker seeds into the EXISTING chat loop. pendingApproval is
// parameterised so one case proves the existing HITL card still renders.
const sendMessage = vi.fn();
let pendingApproval: PendingApproval | null = null;
vi.mock('@/hooks/useConversationFlow', () => ({
  useConversationFlow: vi.fn(),
}));

// Trim the empty-state to the surface under test: the checklist/wizard/help
// pull their own query chains that are irrelevant here.
vi.mock('@/components/onboarding/GettingStartedChecklist', () => ({
  GettingStartedChecklist: () => null,
}));
vi.mock('@/components/onboarding/FirstActionWizard', () => ({
  FirstActionWizard: () => null,
}));
vi.mock('@/components/onboarding/SectionHelp', () => ({
  SectionHelp: () => null,
}));

// A connected channel so the empty-state shows the chips + compose picker
// rather than the connect nudge.
vi.mock('@/lib/api/business-api', () => ({
  bizApi: () => ({
    get: (url: string) => {
      if (url.includes('/integrations')) {
        return Promise.resolve({ data: [{ platform: 'telegram', status: 'active' }] });
      }
      return Promise.resolve({ data: { id: 'conv-1', title: 'Test', projectId: null } });
    },
  }),
}));

function makeClient() {
  return new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: 0 } } });
}

function Wrapper({ children }: { children: ReactNode }) {
  return <QueryClientProvider client={makeClient()}>{children}</QueryClientProvider>;
}

function flowReturn() {
  return {
    messages: [],
    isLoading: false,
    isStreaming: false,
    awaitingTurn: false,
    sendMessage,
    stop: vi.fn(),
    pendingApproval,
    resolveApproval: vi.fn(),
    isResolving: false,
  };
}

beforeEach(() => {
  vi.clearAllMocks();
  pendingApproval = null;
  useBusinessStore.setState({ activeBusinessId: 'biz-1' });
  vi.mocked(useConversationFlow).mockImplementation(() => flowReturn());
});

afterEach(() => {
  vi.restoreAllMocks();
});

describe('ChatWindow — guided compose seeds the existing send path', () => {
  it('reaches compose from the empty-state and calls sendMessage with the composed string', async () => {
    const user = userEvent.setup();
    render(
      <Wrapper>
        <ChatWindow conversationId="conv-1" />
      </Wrapper>
    );

    const trigger = await screen.findByRole('button', { name: 'Составить пост' });
    await user.click(trigger);

    await user.type(screen.getByLabelText('О чём пост'), 'открытие в субботу');
    await user.click(screen.getByRole('button', { name: 'Подготовить в чате' }));

    expect(sendMessage).toHaveBeenCalledTimes(1);
    expect(sendMessage).toHaveBeenCalledWith(
      'Напиши анонс для организации на тему: открытие в субботу. Составь готовый пост.'
    );
  });

  it('renders the existing ToolApprovalCard when a publish tool call is pending (HITL unchanged)', async () => {
    pendingApproval = singleCallBatch;
    render(
      <Wrapper>
        <ChatWindow conversationId="conv-1" />
      </Wrapper>
    );

    expect(
      await screen.findByRole('region', { name: /Ожидает подтверждения/ })
    ).toBeInTheDocument();
  });
});
