import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type { ReactNode } from 'react';
import NewBusinessPage from '@/app/(app)/business/new/page';
import { ProfileForm } from '@/components/business/ProfileForm';
import type { Business } from '@/types/business';

const BIZ_ID = 'test-biz-id';

vi.mock('next/navigation', () => ({ useRouter: () => ({ push: vi.fn() }) }));

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

  it.each(['profile', 'new'])(
    'associates every %s caption with its control and focuses on caption click',
    async (page) => {
      const client = new QueryClient();
      const { container } = render(
        page === 'profile' ? (
          <ProfileForm defaultValues={{ name: 'Test', category: 'cafe' }} />
        ) : (
          <NewBusinessPage />
        ),
        {
          wrapper: wrapper(client),
        }
      );
      for (const id of ['name', 'address', 'phone', 'website', 'description', 'category']) {
        const input = container.querySelector(`#${id}`)!;
        const label = container.querySelector(`label[for="${id}"]`)!;
        expect(input).toHaveAccessibleName();
        expect(label).not.toBeNull();
        if (id !== 'category') {
          await userEvent.click(label);
          expect(input).toHaveFocus();
        }
      }
    }
  );

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
