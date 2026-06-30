import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type { ReactNode } from 'react';
import { ProfileForm } from '@/components/business/ProfileForm';
import type { Business } from '@/types/business';

const BIZ_ID = 'test-biz-id';

vi.mock('@/lib/api/business-api', () => ({
  bizApi: () => ({
    get: vi.fn(),
    post: vi.fn(),
    put: vi.fn().mockResolvedValue({ data: {} }),
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

vi.mock('sonner', () => ({
  toast: { success: vi.fn(), error: vi.fn() },
}));

function wrapper(client: QueryClient) {
  return function Wrapper({ children }: { children: ReactNode }) {
    return <QueryClientProvider client={client}>{children}</QueryClientProvider>;
  };
}

describe('ProfileForm preserves unsaved edits across a BUSINESS_PROFILE refetch', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it('keeps the typed name when defaultValues identity changes (e.g. post-logo-upload refetch)', async () => {
    const user = userEvent.setup();
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });

    const initial: Partial<Business> = { name: 'Old', category: 'cafe' };
    const { rerender } = render(<ProfileForm defaultValues={initial} />, {
      wrapper: wrapper(client),
    });

    const nameInput = screen.getByPlaceholderText('Кофейня «Мята»') as HTMLInputElement;
    await user.clear(nameInput);
    await user.type(nameInput, 'New Draft');
    expect(nameInput.value).toBe('New Draft');

    rerender(
      <ProfileForm defaultValues={{ name: 'Old Refetched', category: 'cafe', logoUrl: 'x' }} />
    );

    expect((screen.getByPlaceholderText('Кофейня «Мята»') as HTMLInputElement).value).toBe(
      'New Draft'
    );
  });
});
