import { describe, expect, it, vi, beforeEach } from 'vitest';
import { render, screen, waitFor, fireEvent } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type { ReactNode } from 'react';
import axeCore from 'axe-core';
import type { AxeResults, ElementContext, RunOptions } from 'axe-core';
import { Sidebar } from '@/components/sidebar';
import { SidebarSearch } from '@/components/sidebar/SidebarSearch';
import { ProjectSection } from '@/components/sidebar/ProjectSection';
import type { Project } from '@/types/project';
import type { Conversation } from '@/lib/conversations';

// Phase 19 / Plan 19-05 — axe-core a11y audit (RESEARCH §3 + threat T-19-05-01).
//
// CI gate (BLOCKING per directive): fails on `critical` AND `serious`
// findings. `moderate` and `minor` violations are logged only — they
// surface in the test output but do not break the build. We filter by
// impact manually because @chialab/vitest-axe's `toHaveNoViolations`
// matcher is impact-agnostic.
//
// We import axe-core's `run()` directly because @chialab/vitest-axe is
// matchers-only (its `expect.extend(matchers)` registration lives in
// vitest.setup.ts, plan 19-05 / Wave 0). axe-core 4.x provides
// `axe.run(context, options)` returning AxeResults. We expose a thin
// `axe(container, opts)` alias below so call sites read identically to
// the planning docs (`axe(container, ...)`).

/**
 * `axe(container)` — thin alias over `axeCore.run(container, {...})`.
 *
 * Phase 19's plan (and RESEARCH §3) cites the call shape `axe(container, ...)`
 * because `@chialab/vitest-axe` was originally documented to re-export that
 * helper. The actual `@chialab/vitest-axe@0.19.1` package exposes ONLY the
 * matcher (`toHaveNoViolations`). The runner has to come from `axe-core`
 * directly. The alias keeps the call site phrasing identical to the
 * planning docs and the plan's grep checks (`axe(container` substring).
 */
function axe(
  container: ElementContext,
  options?: RunOptions
): Promise<AxeResults> {
  return axeCore.run(container, options ?? {});
}

// ----- Mocks (mirror ProjectSection.test.tsx and SidebarSearch.test.tsx) -----

let pathnameValue = '/chat';
vi.mock('next/navigation', () => ({
  usePathname: () => pathnameValue,
  useRouter: () => ({ push: vi.fn(), back: vi.fn(), replace: vi.fn() }),
}));

vi.mock('sonner', () => ({
  toast: { success: vi.fn(), error: vi.fn(), warning: vi.fn() },
}));

const apiGet = vi.fn();
const apiPost = vi.fn();
vi.mock('@/lib/api', () => ({
  api: {
    get: (...args: unknown[]) => apiGet(...args),
    post: (...args: unknown[]) => apiPost(...args),
    put: vi.fn(),
    delete: vi.fn(),
  },
}));

// useConversations / useProjects rely on api.get; default to safe values.
function setupDefaultApi() {
  apiGet.mockImplementation((url: string) => {
    if (url === '/business') {
      return Promise.resolve({ data: { id: 'biz-1', name: 'Business' } });
    }
    if (url === '/conversations') {
      return Promise.resolve({ data: [] });
    }
    if (url === '/projects') {
      return Promise.resolve({ data: [] });
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

// ----- Impact filter (BLOCKING gate per directive) -----

const FAIL_IMPACTS = new Set<string>(['critical', 'serious']);

async function expectNoBlockingViolations(container: ElementContext) {
  // axe-core run options. resultTypes=['violations'] skips the heavy
  // passes/inapplicable lists (RESEARCH §3 line 237).
  const results = await axe(container, { resultTypes: ['violations'] });
  const blocking = results.violations.filter((v) =>
    v.impact ? FAIL_IMPACTS.has(v.impact) : false
  );
  const moderateMinor = results.violations.filter(
    (v) => v.impact === 'moderate' || v.impact === 'minor'
  );
  if (moderateMinor.length > 0) {
    // eslint-disable-next-line no-console
    console.warn(
      'axe non-blocking violations (moderate/minor — logged only):\n' +
        moderateMinor
          .map((v) => `  [${v.impact}] ${v.id} — ${v.help}\n    ${v.helpUrl}`)
          .join('\n')
    );
  }
  if (blocking.length > 0) {
    // eslint-disable-next-line no-console
    console.error(
      'axe BLOCKING violations (critical/serious — fail the build):\n' +
        blocking
          .map((v) => `  [${v.impact}] ${v.id} — ${v.help}\n    ${v.helpUrl}`)
          .join('\n')
    );
  }
  expect(blocking).toEqual([]);
}

// ----- Test fixtures -----

const sampleProject: Project = {
  id: 'p-1',
  businessId: 'b-1',
  name: 'Отзывы',
  description: '',
  systemPrompt: '',
  whitelistMode: 'inherit',
  allowedTools: [],
  quickActions: [],
  createdAt: '2026-04-18T00:00:00Z',
  updatedAt: '2026-04-18T00:00:00Z',
};

function makeConv(id: string, title: string): Conversation {
  return {
    id,
    userId: 'u-1',
    businessId: 'b-1',
    projectId: sampleProject.id,
    title,
    titleStatus: 'auto',
    pinnedAt: null,
    createdAt: '2026-04-18T00:00:00Z',
    updatedAt: '2026-04-18T00:00:00Z',
  };
}

describe('Phase 19 a11y audit — sidebar surfaces (BLOCKING — critical+serious only)', () => {
  beforeEach(() => {
    apiGet.mockReset();
    apiPost.mockReset();
    setupDefaultApi();
    pathnameValue = '/chat';
  });

  it('audits open mobile drawer + chat list — no critical/serious violations', async () => {
    const user = userEvent.setup();
    render(<Sidebar />, { wrapper: Wrapper });
    // Sidebar is mobile-only and starts closed; open the drawer first so
    // the audit covers the OPEN drawer per directive (3 scenarios — one is
    // the open mobile drawer, RESEARCH §3 line 234).
    await user.click(screen.getByRole('button', { name: 'Открыть боковое меню' }));
    await waitFor(() => {
      expect(screen.getByRole('dialog')).toBeInTheDocument();
    });
    // Audit document.body so the Radix Dialog Portal contents are included.
    await expectNoBlockingViolations(document.body);
  });

  it('audits search dropdown with empty results — no critical/serious violations', async () => {
    const user = userEvent.setup();
    render(<SidebarSearch />, { wrapper: Wrapper });
    await user.type(screen.getByRole('combobox'), 'тест');
    await waitFor(() => {
      // Empty-state copy proves the Popover content is open.
      expect(screen.getByText(/Ничего не найдено по «тест»/)).toBeInTheDocument();
    });
    await expectNoBlockingViolations(document.body);
  });

  it('audits ProjectSection with context menu open — no critical/serious violations', async () => {
    const convs = [makeConv('c-1', 'Первый чат'), makeConv('c-2', 'Второй чат')];
    render(
      <Wrapper>
        <ProjectSection project={sampleProject} conversations={convs} />
      </Wrapper>
    );
    // Open the first per-row DropdownMenu trigger («Меню чата …»). Radix
    // renders the menu in a Portal; auditing document.body picks it up.
    const trigger = screen.getAllByRole('button', { name: /Меню чата/ })[0];
    fireEvent.click(trigger);
    await waitFor(() => {
      expect(
        screen.getByRole('menuitem', { name: /Закрепить|Открепить/ })
      ).toBeInTheDocument();
    });
    await expectNoBlockingViolations(document.body);
  });
});
