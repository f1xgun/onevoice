import { beforeEach, describe, expect, it, vi } from 'vitest';
import { act, fireEvent, render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { StrictMode, type ReactNode } from 'react';
import {
  VoiceProfileSection,
  VOICE_PROFILE_MAX_LENGTH,
} from '@/components/business/VoiceProfileSection';
import { QUERY_KEYS } from '@/lib/constants/queryKeys';

const businessState: { activeBusinessId: string | null } = { activeBusinessId: 'business-a' };
const getMock = vi.fn();
const putMock = vi.fn();

vi.mock('@/lib/api/business-api', () => ({
  bizApi: (businessId: string) => ({
    get: (path: string) => getMock(businessId, path),
    put: (path: string, body: unknown) => putMock(businessId, path, body),
  }),
}));
vi.mock('@/lib/stores/business', () => ({
  useBusinessStore: (selector: (state: typeof businessState) => unknown) => selector(businessState),
}));

const permissionState = {
  'business.read': { allowed: true, isLoading: false, isError: false, refetch: vi.fn() },
  'business.update': { allowed: true, isLoading: false, isError: false, refetch: vi.fn() },
};
vi.mock('@/lib/hooks/usePermission', () => ({
  usePermission: (permission: keyof typeof permissionState) => ({ ...permissionState[permission] }),
}));

const toastSuccess = vi.fn();
const toastError = vi.fn();
vi.mock('sonner', () => ({
  toast: {
    success: (...args: unknown[]) => toastSuccess(...args),
    error: (...args: unknown[]) => toastError(...args),
  },
}));

function newClient() {
  return new QueryClient({ defaultOptions: { queries: { retry: false } } });
}
function wrapper(client: QueryClient) {
  return function Wrapper({ children }: { children: ReactNode }) {
    return <QueryClientProvider client={client}>{children}</QueryClientProvider>;
  };
}
function deferred<T>() {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((done) => {
    resolve = done;
  });
  return { promise, resolve };
}

describe('VoiceProfileSection', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    businessState.activeBusinessId = 'business-a';
    Object.assign(permissionState['business.read'], {
      allowed: true,
      isLoading: false,
      isError: false,
    });
    Object.assign(permissionState['business.update'], {
      allowed: true,
      isLoading: false,
      isError: false,
    });
    getMock.mockImplementation((businessId: string) =>
      Promise.resolve({ data: { voiceProfile: `profile-${businessId}` } })
    );
    putMock.mockResolvedValue({ data: {} });
  });

  it('parses and displays the organization-scoped response', async () => {
    render(<VoiceProfileSection />, { wrapper: wrapper(newClient()) });
    expect(await screen.findByRole<HTMLTextAreaElement>('textbox')).toHaveValue(
      'profile-business-a'
    );
    expect(getMock).toHaveBeenCalledWith('business-a', '/voice-profile');
  });

  it('captures the organization ID in the save request', async () => {
    const user = userEvent.setup();
    render(<VoiceProfileSection />, { wrapper: wrapper(newClient()) });
    const textarea = await screen.findByRole<HTMLTextAreaElement>('textbox');
    await user.clear(textarea);
    await user.type(textarea, 'Warm and concise');
    await user.click(screen.getByRole('button', { name: 'Сохранить профиль' }));
    await waitFor(() =>
      expect(putMock).toHaveBeenCalledWith('business-a', '/voice-profile', {
        voiceProfile: 'Warm and concise',
      })
    );
  });

  it('remounts with B data when a dirty A editor switches organizations', async () => {
    const user = userEvent.setup();
    const view = render(<VoiceProfileSection />, { wrapper: wrapper(newClient()) });
    const textareaA = await screen.findByRole<HTMLTextAreaElement>('textbox');
    await user.clear(textareaA);
    await user.type(textareaA, 'unsaved A');
    businessState.activeBusinessId = 'business-b';
    view.rerender(<VoiceProfileSection />);
    await waitFor(() => expect(screen.getByRole('textbox')).toHaveValue('profile-business-b'));
    expect(screen.getByRole('textbox')).not.toHaveValue('unsaved A');
  });

  it('does not reset B, toast, or invalidate B when a delayed A save completes', async () => {
    const user = userEvent.setup();
    const saveA = deferred<{ data: Record<string, never> }>();
    putMock.mockImplementation((businessId: string) =>
      businessId === 'business-a' ? saveA.promise : Promise.resolve({ data: {} })
    );
    const client = newClient();
    const invalidateSpy = vi.spyOn(client, 'invalidateQueries');
    const view = render(<VoiceProfileSection />, { wrapper: wrapper(client) });
    const textareaA = await screen.findByRole<HTMLTextAreaElement>('textbox');
    await user.clear(textareaA);
    await user.type(textareaA, 'saved A');
    await user.click(screen.getByRole('button', { name: 'Сохранить профиль' }));
    businessState.activeBusinessId = 'business-b';
    view.rerender(<VoiceProfileSection />);
    const textareaB = await screen.findByRole<HTMLTextAreaElement>('textbox');
    await user.clear(textareaB);
    await user.type(textareaB, 'unsaved B');
    saveA.resolve({ data: {} });
    await waitFor(() =>
      expect(invalidateSpy).toHaveBeenCalledWith({
        queryKey: QUERY_KEYS.BUSINESS_VOICE_PROFILE('business-a'),
      })
    );
    expect(invalidateSpy).toHaveBeenCalledWith({
      queryKey: QUERY_KEYS.BUSINESS_PROFILE('business-a'),
    });
    expect(invalidateSpy).not.toHaveBeenCalledWith({
      queryKey: QUERY_KEYS.BUSINESS_VOICE_PROFILE('business-b'),
    });
    expect(screen.getByRole('textbox')).toHaveValue('unsaved B');
    expect(toastSuccess).not.toHaveBeenCalled();
  });

  it('counts Unicode code points like the Go server and blocks 401 characters', async () => {
    render(<VoiceProfileSection />, { wrapper: wrapper(newClient()) });
    const textarea = await screen.findByRole<HTMLTextAreaElement>('textbox');
    fireEvent.change(textarea, { target: { value: '😀'.repeat(VOICE_PROFILE_MAX_LENGTH) } });
    expect(
      screen.getByText(`${VOICE_PROFILE_MAX_LENGTH} из ${VOICE_PROFILE_MAX_LENGTH}`)
    ).toBeVisible();
    expect(textarea).toHaveAttribute('aria-invalid', 'false');
    fireEvent.change(textarea, { target: { value: '😀'.repeat(VOICE_PROFILE_MAX_LENGTH + 1) } });
    await waitFor(() => expect(textarea).toHaveAttribute('aria-invalid', 'true'));
    expect(screen.getByRole('button', { name: 'Сохранить профиль' })).toBeDisabled();
  });

  it('hides cached content when read permission is revoked', async () => {
    const view = render(<VoiceProfileSection />, { wrapper: wrapper(newClient()) });
    expect(await screen.findByRole('textbox')).toBeVisible();
    permissionState['business.read'].allowed = false;
    view.rerender(<VoiceProfileSection />);
    expect(screen.queryByRole('textbox')).not.toBeInTheDocument();
  });

  it('shows retry UI for read permission errors and never starts the profile query', async () => {
    permissionState['business.read'].allowed = false;
    permissionState['business.read'].isError = true;
    render(<VoiceProfileSection />, { wrapper: wrapper(newClient()) });
    expect(await screen.findByRole('alert')).toBeVisible();
    expect(getMock).not.toHaveBeenCalled();
  });

  it('hides stale content and reports malformed API responses', async () => {
    getMock.mockResolvedValue({ data: { voiceProfile: 42 } });
    render(<VoiceProfileSection />, { wrapper: wrapper(newClient()) });
    expect(await screen.findByText('Не получилось загрузить профиль голоса')).toBeVisible();
    expect(screen.queryByRole('textbox')).not.toBeInTheDocument();
  });

  it.each([
    ['ru', 'Профиль голоса', 'Сохранить профиль'],
    ['en', 'Voice profile', 'Save profile'],
  ] as const)('renders the editor copy in %s', async (locale, label, button) => {
    globalThis.__setTestLocale(locale);
    render(<VoiceProfileSection />, { wrapper: wrapper(newClient()) });
    expect(await screen.findByRole('textbox', { name: label })).toBeVisible();
    expect(screen.getByRole('button', { name: button })).toBeVisible();
  });
});

describe('VoiceProfileSection review regressions', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    businessState.activeBusinessId = 'business-a';
    Object.assign(permissionState['business.read'], {
      allowed: true,
      isLoading: false,
      isError: false,
    });
    Object.assign(permissionState['business.update'], {
      allowed: true,
      isLoading: false,
      isError: false,
    });
    getMock.mockResolvedValue({ data: { voiceProfile: 'persisted' } });
    putMock.mockResolvedValue({ data: {} });
  });

  it('resets and toasts after a save mounted under React StrictMode', async () => {
    const user = userEvent.setup();
    render(
      <StrictMode>
        <VoiceProfileSection />
      </StrictMode>,
      { wrapper: wrapper(newClient()) }
    );
    const textarea = await screen.findByRole('textbox');
    await user.clear(textarea);
    await user.type(textarea, 'saved in strict mode');
    await user.click(screen.getByRole('button', { name: 'Сохранить профиль' }));
    await waitFor(() => expect(toastSuccess).toHaveBeenCalledWith('Профиль голоса сохранён'));
    expect(screen.getByRole('button', { name: 'Сохранить профиль' })).toBeDisabled();
  });

  it('accepts refetched persisted data only while the editor is clean', async () => {
    const user = userEvent.setup();
    const client = newClient();
    render(<VoiceProfileSection />, { wrapper: wrapper(client) });
    const textarea = await screen.findByRole('textbox');
    await waitFor(() => expect(textarea).toHaveValue('persisted'));

    client.setQueryData(QUERY_KEYS.BUSINESS_VOICE_PROFILE('business-a'), 'refetched clean');
    await waitFor(() => expect(textarea).toHaveValue('refetched clean'));

    await user.clear(textarea);
    await user.type(textarea, 'newer dirty text');
    client.setQueryData(QUERY_KEYS.BUSINESS_VOICE_PROFILE('business-a'), 'older refetch');
    await waitFor(() => expect(textarea).toHaveValue('newer dirty text'));
  });

  it('guards submitted events when editing is denied', async () => {
    permissionState['business.update'].allowed = false;
    render(<VoiceProfileSection />, { wrapper: wrapper(newClient()) });
    const form = (await screen.findByRole('textbox')).closest('form');
    fireEvent.submit(form!);
    expect(putMock).not.toHaveBeenCalled();
  });

  it('shows a status skeleton while profile data loads', async () => {
    getMock.mockReturnValue(deferred<never>().promise);
    render(<VoiceProfileSection />, { wrapper: wrapper(newClient()) });
    expect(screen.getByRole('status', { hidden: true })).toHaveAttribute('aria-hidden', 'true');
    expect(await screen.findByRole('status', { name: 'Загрузка…' })).toBeVisible();
  });

  it('offers the localized retry action after a read error', async () => {
    const user = userEvent.setup();
    getMock.mockRejectedValueOnce(new Error('failed')).mockResolvedValueOnce({
      data: { voiceProfile: 'after retry' },
    });
    render(<VoiceProfileSection />, { wrapper: wrapper(newClient()) });
    await user.click(await screen.findByRole('button', { name: 'Повторить' }));
    expect(await screen.findByRole('textbox')).toHaveValue('after retry');
    expect(getMock).toHaveBeenCalledTimes(2);
  });
});

describe('VoiceProfileSection pending submit guard', () => {
  it('does not start a second mutation while a save is pending', async () => {
    businessState.activeBusinessId = 'business-a';
    Object.assign(permissionState['business.read'], {
      allowed: true,
      isLoading: false,
      isError: false,
    });
    Object.assign(permissionState['business.update'], {
      allowed: true,
      isLoading: false,
      isError: false,
    });
    getMock.mockResolvedValue({ data: { voiceProfile: 'persisted' } });
    const pendingSave = deferred<{ data: Record<string, never> }>();
    putMock.mockReturnValue(pendingSave.promise);
    const user = userEvent.setup();
    render(<VoiceProfileSection />, { wrapper: wrapper(newClient()) });
    const textarea = await screen.findByRole('textbox');
    await user.clear(textarea);
    await user.type(textarea, 'save once');
    const form = textarea.closest('form')!;
    fireEvent.submit(form);
    await waitFor(() => expect(screen.getByRole('button', { name: 'Сохраняем…' })).toBeDisabled());
    fireEvent.submit(form);
    expect(putMock).toHaveBeenCalledTimes(1);
    await act(async () => {
      pendingSave.resolve({ data: {} });
      await pendingSave.promise;
    });
  });
});

describe('VoiceProfileSection successful-save cache consistency', () => {
  it('keeps the saved text visible while the invalidation refetch is delayed', async () => {
    vi.clearAllMocks();
    businessState.activeBusinessId = 'business-a';
    Object.assign(permissionState['business.read'], {
      allowed: true,
      isLoading: false,
      isError: false,
    });
    Object.assign(permissionState['business.update'], {
      allowed: true,
      isLoading: false,
      isError: false,
    });
    const delayedRefetch = deferred<{ data: { voiceProfile: string } }>();
    getMock
      .mockResolvedValueOnce({ data: { voiceProfile: 'old persisted text' } })
      .mockReturnValueOnce(delayedRefetch.promise);
    putMock.mockResolvedValue({ data: {} });
    const user = userEvent.setup();
    render(<VoiceProfileSection />, { wrapper: wrapper(newClient()) });
    const textarea = await screen.findByRole('textbox');
    await user.clear(textarea);
    await user.type(textarea, 'just saved text');
    await user.click(screen.getByRole('button', { name: 'Сохранить профиль' }));

    await waitFor(() => expect(getMock).toHaveBeenCalledTimes(2));
    expect(textarea).toHaveValue('just saved text');
    expect(toastSuccess).toHaveBeenCalledWith('Профиль голоса сохранён');

    await act(async () => {
      delayedRefetch.resolve({ data: { voiceProfile: 'just saved text' } });
      await delayedRefetch.promise;
    });
  });
});
