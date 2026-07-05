import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, waitFor, fireEvent } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type { ReactNode } from 'react';
import {
  VoiceProfileSection,
  VOICE_PROFILE_MAX_LENGTH,
} from '@/components/business/VoiceProfileSection';

const BIZ_ID = 'test-biz-id';

const getMock = vi.fn();
const putMock = vi.fn();

vi.mock('@/lib/api/business-api', () => ({
  bizApi: () => ({
    get: getMock,
    post: vi.fn(),
    put: putMock,
    patch: vi.fn(),
    delete: vi.fn(),
  }),
}));

vi.mock('@/lib/stores/business', () => ({
  useBusinessStore: (selector: (s: { activeBusinessId: string | null }) => unknown) =>
    selector({ activeBusinessId: BIZ_ID }),
}));

vi.mock('@/lib/hooks/usePermission', () => ({
  usePermission: () => ({ allowed: true, isLoading: false }),
}));

const toastSuccess = vi.fn();
const toastError = vi.fn();
vi.mock('sonner', () => ({
  toast: {
    success: (...args: unknown[]) => toastSuccess(...args),
    error: (...args: unknown[]) => toastError(...args),
  },
}));

function wrapper(client: QueryClient) {
  return function Wrapper({ children }: { children: ReactNode }) {
    return <QueryClientProvider client={client}>{children}</QueryClientProvider>;
  };
}

function newClient() {
  return new QueryClient({ defaultOptions: { queries: { retry: false } } });
}

describe('VoiceProfileSection', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it('loads the stored profile via GET /voice-profile and shows it', async () => {
    getMock.mockResolvedValue({ data: { voiceProfile: 'Пиши тепло, без эмодзи.' } });

    render(<VoiceProfileSection />, { wrapper: wrapper(newClient()) });

    const textarea = await screen.findByRole<HTMLTextAreaElement>('textbox');
    await waitFor(() => expect(textarea.value).toBe('Пиши тепло, без эмодзи.'));
    expect(getMock).toHaveBeenCalledWith('/voice-profile');
  });

  it('saves an edited profile via PUT /voice-profile with a { voiceProfile } body', async () => {
    const user = userEvent.setup();
    getMock.mockResolvedValue({ data: { voiceProfile: '' } });
    putMock.mockResolvedValue({ data: {} });

    render(<VoiceProfileSection />, { wrapper: wrapper(newClient()) });

    const textarea = await screen.findByRole<HTMLTextAreaElement>('textbox');
    await waitFor(() => expect(textarea).not.toBeDisabled());
    await user.type(textarea, 'Дружелюбно и коротко');

    const saveBtn = screen.getByRole('button');
    await user.click(saveBtn);

    await waitFor(() =>
      expect(putMock).toHaveBeenCalledWith('/voice-profile', {
        voiceProfile: 'Дружелюбно и коротко',
      })
    );
    await waitFor(() => expect(toastSuccess).toHaveBeenCalled());
  });

  it('enforces the 400-char cap: over-cap blocks the save button and PUT is never sent', async () => {
    getMock.mockResolvedValue({ data: { voiceProfile: '' } });
    putMock.mockResolvedValue({ data: {} });

    render(<VoiceProfileSection />, { wrapper: wrapper(newClient()) });

    const textarea = await screen.findByRole<HTMLTextAreaElement>('textbox');
    await waitFor(() => expect(textarea).not.toBeDisabled());
    const overCap = 'x'.repeat(VOICE_PROFILE_MAX_LENGTH + 50);
    fireEvent.change(textarea, { target: { value: overCap } });

    const saveBtn = screen.getByRole('button');
    expect(saveBtn).toBeDisabled();
    expect(textarea.getAttribute('aria-invalid')).toBe('true');

    fireEvent.click(saveBtn);
    expect(putMock).not.toHaveBeenCalled();
  });
});
