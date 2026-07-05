import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type { ReactNode } from 'react';

import { ChatWindow } from '../ChatWindow';
import { useAuthStore } from '@/lib/auth';
import { trackEvent } from '@/lib/telemetry';

vi.mock('@/lib/telemetry', () => ({
  trackEvent: vi.fn(),
}));

vi.mock('sonner', () => ({
  toast: { success: vi.fn(), error: vi.fn() },
}));

vi.mock('@/lib/hooks/usePermission', () => ({
  usePermission: () => ({ allowed: true, isLoading: false }),
}));

// Reduce the empty-state to a lone checklist so the wiring under test — the
// firstAction CTA calling onOpenWizard — is the only interactive surface. The
// stub renders a button that invokes the prop the ChatWindow passes in, which
// is exactly the seam GettingStartedChecklist exposes on that step.
vi.mock('@/components/onboarding/GettingStartedChecklist', () => ({
  GettingStartedChecklist: ({ onOpenWizard }: { onOpenWizard?: () => void }) => (
    <button type="button" onClick={onOpenWizard}>
      first-action-cta
    </button>
  ),
}));

// Stub the controlled wizard so the test asserts the open state the ChatWindow
// owns, without pulling the wizard's own query chain into this fixture.
vi.mock('@/components/onboarding/FirstActionWizard', () => ({
  FirstActionWizard: ({ open }: { open: boolean; onClose: () => void }) =>
    open ? <div data-testid="first-action-wizard">wizard-open</div> : null,
}));

vi.mock('@/lib/api', () => ({
  api: {
    get: (url: string) => {
      if (url.startsWith('/conversations/')) {
        return Promise.resolve({
          data: { id: 'conv-1', title: 'Test Conversation', projectId: null },
        });
      }
      if (url === '/projects') {
        return Promise.resolve({ data: [] });
      }
      return Promise.resolve({ data: null });
    },
    post: vi.fn(),
    put: vi.fn(),
    delete: vi.fn(),
  },
}));

function makeClient() {
  return new QueryClient({
    defaultOptions: { queries: { retry: false, staleTime: 0 } },
  });
}

function Wrapper({ children }: { children: ReactNode }) {
  const client = makeClient();
  return <QueryClientProvider client={client}>{children}</QueryClientProvider>;
}

function mockGetMessages(responseBody: unknown) {
  const fetchMock = vi.fn().mockImplementation(async () => {
    return new Response(JSON.stringify(responseBody), {
      status: 200,
      headers: { 'Content-Type': 'application/json' },
    });
  });
  vi.stubGlobal('fetch', fetchMock);
  return fetchMock;
}

describe('ChatWindow — first-action wizard wiring (2B)', () => {
  beforeEach(() => {
    vi.restoreAllMocks();
    vi.mocked(trackEvent).mockClear();
    useAuthStore.setState({
      user: null,
      accessToken: 'test-token',
      isAuthenticated: true,
    });
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  it('the checklist firstAction CTA opens the FirstActionWizard via onOpenWizard', async () => {
    mockGetMessages({ messages: [], pendingApprovals: [] });
    const user = userEvent.setup();
    render(
      <Wrapper>
        <ChatWindow conversationId="conv-1" />
      </Wrapper>
    );

    const cta = await screen.findByRole('button', { name: 'first-action-cta' });
    expect(screen.queryByTestId('first-action-wizard')).not.toBeInTheDocument();

    await user.click(cta);

    expect(screen.getByTestId('first-action-wizard')).toBeInTheDocument();
  });

  it('emits an activation/open_wizard telemetry event on the CTA click', async () => {
    mockGetMessages({ messages: [], pendingApprovals: [] });
    const user = userEvent.setup();
    render(
      <Wrapper>
        <ChatWindow conversationId="conv-1" />
      </Wrapper>
    );

    const cta = await screen.findByRole('button', { name: 'first-action-cta' });
    await user.click(cta);

    expect(trackEvent).toHaveBeenCalledWith('activation', 'open_wizard', {
      metadata: { source: 'getting_started_checklist' },
    });
  });
});
