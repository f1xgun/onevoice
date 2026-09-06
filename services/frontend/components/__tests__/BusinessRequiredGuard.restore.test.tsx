import { beforeEach, expect, it, vi } from 'vitest';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { BusinessRequiredGuard } from '@/components/BusinessRequiredGuard';
import { useBusinessStore } from '@/lib/stores/business';
import { useBusinessDeletionStore } from '@/lib/stores/businessDeletion';
import { DeleteBusinessModal } from '@/components/business/DeleteBusinessModal';
import { api } from '@/lib/api';
import { authFetch } from '@/lib/api/authFetch';

const navigation = vi.hoisted(() => ({ replace: vi.fn(), push: vi.fn() }));
vi.mock('next/navigation', () => ({
  useRouter: () => navigation,
  usePathname: () => '/chat',
}));
vi.mock('@/lib/api', () => ({ api: { get: vi.fn() } }));
vi.mock('@/lib/api/authFetch', () => ({ authFetch: vi.fn() }));
vi.mock('sonner', () => ({ toast: { success: vi.fn(), error: vi.fn() } }));

beforeEach(() => {
  vi.clearAllMocks();
  useBusinessStore.getState().clear();
  useBusinessDeletionStore.getState().clear();
});

it('restores the last organization after delete and reload with empty client stores', async () => {
  let pending = false;
  vi.mocked(api.get).mockImplementation(async () => ({
    data: [
      {
        id: 'org-1',
        name: 'Organization',
        role: { id: 'owner', name: 'Owner' },
        status: 'active',
        joined_at: '2026-09-01T00:00:00Z',
        ...(pending ? { deletion_pending_until: '2026-10-06T12:00:00Z' } : {}),
      },
    ],
  }));
  vi.mocked(authFetch).mockImplementation(async (_, options) => {
    pending = options?.method === 'DELETE';
    return new Response(null, { status: 204 });
  });
  function mount(showDelete = false) {
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    return render(
      <QueryClientProvider client={client}>
        <BusinessRequiredGuard>
          <div>protected</div>
          {showDelete && (
            <DeleteBusinessModal
              open
              onOpenChange={vi.fn()}
              businessId="org-1"
              businessName="Organization"
              otherBusinessIds={[]}
            />
          )}
        </BusinessRequiredGuard>
      </QueryClientProvider>
    );
  }
  const first = mount(true);
  await screen.findByText('protected');
  fireEvent.change(await screen.findByRole('textbox'), { target: { value: 'Organization' } });
  fireEvent.click(screen.getByRole('button', { name: 'Удалить организацию' }));
  await waitFor(() => expect(navigation.push).toHaveBeenCalledWith('/business'));
  expect(pending).toBe(true);
  first.unmount();
  useBusinessStore.getState().clear();
  useBusinessDeletionStore.getState().clear();
  mount();
  const restore = await screen.findByRole('button');
  expect(screen.queryByText('protected')).toBeNull();
  expect(navigation.replace).not.toHaveBeenCalled();
  fireEvent.click(restore);
  await screen.findByText('protected');
  await waitFor(() => expect(screen.queryByRole('alert')).toBeNull());
  expect(authFetch).toHaveBeenLastCalledWith('/api/v1/businesses/org-1/restore', {
    method: 'POST',
    credentials: 'include',
  });
  expect(useBusinessStore.getState().activeBusinessId).toBe('org-1');
});
