import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';

import { LanguageSwitcher } from '@/components/ui/LanguageSwitcher';
import { toast } from 'sonner';

// Mock next/navigation's useRouter so we can spy on `refresh()` and prove
// the switcher re-runs the request config after the cookie is updated.
const refreshMock = vi.fn();
vi.mock('next/navigation', () => ({
  useRouter: () => ({
    refresh: refreshMock,
    push: vi.fn(),
    replace: vi.fn(),
    back: vi.fn(),
  }),
}));

// Mock sonner so failure paths can assert toast.error was called.
vi.mock('sonner', () => ({
  toast: { error: vi.fn(), success: vi.fn(), warning: vi.fn() },
}));

// vitest.setup.ts globally mocks 'next-intl' with useLocale: () => 'ru'.
// That default is what we want here — picking "English" exercises the
// change path. The setup mock is sufficient; no per-test override required.

describe('LanguageSwitcher', () => {
  let fetchSpy: ReturnType<typeof vi.spyOn>;

  beforeEach(() => {
    refreshMock.mockReset();
    vi.mocked(toast.error).mockReset();
    fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValue(new Response(null, { status: 204 }));
  });

  afterEach(() => {
    fetchSpy.mockRestore();
  });

  it('renders a globe trigger labelled "Language"', () => {
    render(<LanguageSwitcher />);
    const trigger = screen.getByLabelText('Language');
    expect(trigger).toBeInTheDocument();
    expect(trigger).toHaveAttribute('aria-haspopup', 'menu');
  });

  it('POSTs /api/locale with the chosen locale and refreshes the router', async () => {
    const user = userEvent.setup();
    render(<LanguageSwitcher />);

    // Open the dropdown and pick the English endonym.
    await user.click(screen.getByLabelText('Language'));
    const enItem = await screen.findByText('English');
    await user.click(enItem);

    // The POST is dispatched inside startTransition; awaiting microtasks
    // flushes the inner async fn before we assert.
    await vi.waitFor(() => {
      expect(fetchSpy).toHaveBeenCalled();
    });

    const [url, init] = fetchSpy.mock.calls[0] as [string, RequestInit];
    expect(url).toBe('/api/locale');
    expect(init.method).toBe('POST');
    expect(String(init.body)).toContain('"locale":"en"');

    await vi.waitFor(() => {
      expect(refreshMock).toHaveBeenCalled();
    });
    expect(toast.error).not.toHaveBeenCalled();
  });

  it('does NOT fire when the user picks the already-active locale', async () => {
    const user = userEvent.setup();
    render(<LanguageSwitcher />);

    await user.click(screen.getByLabelText('Language'));
    const ruItem = await screen.findByText('Русский');
    await user.click(ruItem);

    // The component short-circuits when next === active to avoid pointless
    // round-trips.
    expect(fetchSpy).not.toHaveBeenCalled();
    expect(refreshMock).not.toHaveBeenCalled();
  });

  it('shows a sonner error and suppresses router.refresh when /api/locale returns 5xx', async () => {
    fetchSpy.mockResolvedValueOnce(new Response(null, { status: 500 }));
    const user = userEvent.setup();
    render(<LanguageSwitcher />);

    await user.click(screen.getByLabelText('Language'));
    const enItem = await screen.findByText('English');
    await user.click(enItem);

    await vi.waitFor(() => {
      expect(toast.error).toHaveBeenCalled();
    });
    expect(refreshMock).not.toHaveBeenCalled();
  });

  it('shows a sonner error and suppresses router.refresh when fetch rejects', async () => {
    fetchSpy.mockRejectedValueOnce(new TypeError('network down'));
    const user = userEvent.setup();
    render(<LanguageSwitcher />);

    await user.click(screen.getByLabelText('Language'));
    const enItem = await screen.findByText('English');
    await user.click(enItem);

    await vi.waitFor(() => {
      expect(toast.error).toHaveBeenCalled();
    });
    expect(refreshMock).not.toHaveBeenCalled();
  });
});
