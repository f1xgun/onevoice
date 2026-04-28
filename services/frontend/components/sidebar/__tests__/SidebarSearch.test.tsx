import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, waitFor, act, cleanup } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type { ReactNode } from 'react';
import { SidebarSearch } from '../SidebarSearch';

// next/navigation — controllable pathname.
let pathnameValue = '/chat';
vi.mock('next/navigation', () => ({
  usePathname: () => pathnameValue,
  useRouter: () => ({ push: vi.fn(), back: vi.fn(), replace: vi.fn() }),
}));

// API client. apiGet's responses are configured per-test.
const apiGet = vi.fn();
vi.mock('@/lib/api', () => ({
  api: {
    get: (...args: unknown[]) => apiGet(...args),
    post: vi.fn(),
    put: vi.fn(),
    delete: vi.fn(),
  },
}));

function setupBusinessAndDefaultSearch() {
  apiGet.mockImplementation((url: string) => {
    if (url === '/business') {
      return Promise.resolve({ data: { id: 'biz-1', name: 'Test Business' } });
    }
    if (url === '/search') {
      return Promise.resolve({ data: [] });
    }
    return Promise.resolve({ data: null });
  });
}

function makeClient() {
  return new QueryClient({ defaultOptions: { queries: { retry: false } } });
}

function Wrapper({ children }: { children: ReactNode }) {
  const client = makeClient();
  return <QueryClientProvider client={client}>{children}</QueryClientProvider>;
}

function setMacUA() {
  Object.defineProperty(window.navigator, 'platform', {
    configurable: true,
    value: 'MacIntel',
  });
}

function setLinuxUA() {
  Object.defineProperty(window.navigator, 'platform', {
    configurable: true,
    value: 'Linux x86_64',
  });
  Object.defineProperty(window.navigator, 'userAgent', {
    configurable: true,
    value: 'Mozilla/5.0 (X11; Linux x86_64)',
  });
}

describe('SidebarSearch — Phase 19 / Plan 19-04 / SEARCH-04 + UI-06', () => {
  beforeEach(() => {
    apiGet.mockReset();
    setupBusinessAndDefaultSearch();
    pathnameValue = '/chat';
  });

  afterEach(() => {
    cleanup();
    vi.useRealTimers();
  });

  it('renders Mac placeholder «Поиск... ⌘K» on Mac UA', async () => {
    setMacUA();
    render(
      <Wrapper>
        <SidebarSearch />
      </Wrapper>
    );
    expect(screen.getByPlaceholderText('Поиск... ⌘K')).toBeInTheDocument();
  });

  it('renders Linux placeholder «Поиск... Ctrl-K» on non-Mac UA', () => {
    setLinuxUA();
    render(
      <Wrapper>
        <SidebarSearch />
      </Wrapper>
    );
    expect(screen.getByPlaceholderText('Поиск... Ctrl-K')).toBeInTheDocument();
  });

  it('does NOT fire /search when query is 1 char (D-13 min length 2)', async () => {
    const user = userEvent.setup();
    render(
      <Wrapper>
        <SidebarSearch />
      </Wrapper>
    );
    const input = screen.getByRole('combobox');
    await user.type(input, 'a');

    // Wait long enough that any debounce would have fired.
    await new Promise((r) => setTimeout(r, 500));

    const searchCalls = apiGet.mock.calls.filter((c) => c[0] === '/search');
    expect(searchCalls).toHaveLength(0);
  });

  it('fires /search after the 250 ms debounce when query >= 2 chars', async () => {
    const user = userEvent.setup();
    render(
      <Wrapper>
        <SidebarSearch />
      </Wrapper>
    );
    const input = screen.getByRole('combobox');
    await user.type(input, 'тест');

    // Allow debounce + tanstack-query fetch to land.
    await waitFor(
      () => {
        const calls = apiGet.mock.calls.filter((c) => c[0] === '/search');
        expect(calls.length).toBeGreaterThanOrEqual(1);
      },
      { timeout: 2000 }
    );

    // Last call should carry q='тест' and limit=20 (no project_id at /chat root).
    const last = apiGet.mock.calls.filter((c) => c[0] === '/search').slice(-1)[0];
    const params = last?.[1]?.params as Record<string, unknown> | undefined;
    expect(params?.q).toBe('тест');
    expect(params?.limit).toBe(20);
    expect(params?.project_id).toBeUndefined();
  });

  it('focuses + selects input on onevoice:sidebar-search-focus event (Cmd-K consumer)', async () => {
    render(
      <Wrapper>
        <SidebarSearch />
      </Wrapper>
    );
    const input = screen.getByRole('combobox') as HTMLInputElement;
    expect(document.activeElement).not.toBe(input);

    await act(async () => {
      window.dispatchEvent(new CustomEvent('onevoice:sidebar-search-focus'));
    });

    expect(document.activeElement).toBe(input);
  });

  it('Esc clears input + closes popover + blurs (D-11)', async () => {
    const user = userEvent.setup();
    render(
      <Wrapper>
        <SidebarSearch />
      </Wrapper>
    );
    const input = screen.getByRole('combobox') as HTMLInputElement;
    await user.click(input);
    await user.type(input, 'hello');
    expect(input.value).toBe('hello');

    await user.keyboard('{Escape}');

    expect(input.value).toBe('');
    expect(document.activeElement).not.toBe(input);
  });

  it('shows «По всему бизнесу» checkbox on /chat/projects/{id} (D-10)', async () => {
    pathnameValue = '/chat/projects/p-42';
    const user = userEvent.setup();
    render(
      <Wrapper>
        <SidebarSearch />
      </Wrapper>
    );
    const input = screen.getByRole('combobox');
    await user.type(input, 'абв');

    await waitFor(() => {
      expect(screen.getByText('По всему бизнесу')).toBeInTheDocument();
    });

    // Default scope sends project_id=p-42.
    await waitFor(() => {
      const last = apiGet.mock.calls.filter((c) => c[0] === '/search').slice(-1)[0];
      expect(last?.[1]?.params?.project_id).toBe('p-42');
    });
  });

  it('omits project_id from /search when «По всему бизнесу» is toggled on (D-10)', async () => {
    pathnameValue = '/chat/projects/p-42';
    const user = userEvent.setup();
    render(
      <Wrapper>
        <SidebarSearch />
      </Wrapper>
    );
    const input = screen.getByRole('combobox');
    await user.type(input, 'абв');

    await waitFor(() => {
      expect(screen.getByText('По всему бизнесу')).toBeInTheDocument();
    });
    const checkbox = screen.getByRole('checkbox');
    await user.click(checkbox);

    await waitFor(() => {
      const last = apiGet.mock.calls.filter((c) => c[0] === '/search').slice(-1)[0];
      expect(last?.[1]?.params?.project_id).toBeUndefined();
    });
  });

  it('does NOT render «По всему бизнесу» checkbox at /chat root (D-10)', async () => {
    pathnameValue = '/chat';
    const user = userEvent.setup();
    render(
      <Wrapper>
        <SidebarSearch />
      </Wrapper>
    );
    const input = screen.getByRole('combobox');
    await user.type(input, 'абв');

    // Wait for popover to open.
    await waitFor(() => {
      expect(screen.queryByText(/Ничего не найдено по/)).toBeInTheDocument();
    });
    expect(screen.queryByText('По всему бизнесу')).not.toBeInTheDocument();
  });

  it('shows empty state «Ничего не найдено по «{query}»» when results empty', async () => {
    const user = userEvent.setup();
    render(
      <Wrapper>
        <SidebarSearch />
      </Wrapper>
    );
    const input = screen.getByRole('combobox');
    await user.type(input, 'нетрезультатов');

    await waitFor(() => {
      expect(screen.getByText(/Ничего не найдено по «нетрезультатов»/)).toBeInTheDocument();
    });
  });

  it('T-19-LOG-LEAK frontend: does NOT log the literal query to console.* during a search', async () => {
    const logSpy = vi.spyOn(console, 'log').mockImplementation(() => {});
    const warnSpy = vi.spyOn(console, 'warn').mockImplementation(() => {});
    const errorSpy = vi.spyOn(console, 'error').mockImplementation(() => {});

    const user = userEvent.setup();
    render(
      <Wrapper>
        <SidebarSearch />
      </Wrapper>
    );
    const input = screen.getByRole('combobox');
    const sensitive = 'конфиденциальныйпоиск42';
    await user.type(input, sensitive);

    await waitFor(() => {
      const calls = apiGet.mock.calls.filter((c) => c[0] === '/search');
      expect(calls.length).toBeGreaterThanOrEqual(1);
    });

    const allCallsArgs: unknown[] = [
      ...logSpy.mock.calls.flat(),
      ...warnSpy.mock.calls.flat(),
      ...errorSpy.mock.calls.flat(),
    ];
    for (const arg of allCallsArgs) {
      const repr = typeof arg === 'string' ? arg : JSON.stringify(arg);
      expect(repr).not.toContain(sensitive);
    }

    logSpy.mockRestore();
    warnSpy.mockRestore();
    errorSpy.mockRestore();
  });
});
