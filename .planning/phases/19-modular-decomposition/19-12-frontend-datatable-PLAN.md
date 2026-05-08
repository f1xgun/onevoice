---
plan: 19-12
phase: 19
slug: frontend-datatable
wave: 5
depends_on: []
files_modified:
  - services/frontend/app/(app)/posts/page.tsx
files_created:
  - services/frontend/components/lists/DataTable.tsx
  - services/frontend/hooks/useDataTableFilters.ts
  - services/frontend/hooks/useDataTableSearch.ts
  - services/frontend/components/lists/__tests__/DataTable.test.tsx
  - services/frontend/hooks/__tests__/useDataTableFilters.test.ts
  - services/frontend/hooks/__tests__/useDataTableSearch.test.ts
files_deleted: []
success_criteria: [SC-01, SC-03]
autonomous: true
estimated_loc_delta: -200 / +300
---

## Plan Goal

Extract the table UI primitives currently duplicated across `posts/page.tsx` (567 LOC) and similar list pages into composition primitives:

- `<DataTable<T>>` — generic table component (header row + scroll-x wrapper + skeleton + empty state + optional row expand)
- `useDataTableFilters<F>()` — filter state + URLSearchParams query-string builder
- `useDataTableSearch<T>()` — debounced text search over caller-supplied searchable fields

**Pilot scope (RESEARCH §11 + CONTEXT.md deferred-ideas item):** Adopt on `posts/page.tsx` only in this phase. Do NOT migrate `tasks/page.tsx` (real-time SSE feed — different shape) or `integrations/page.tsx` (card grid, not tabular). Reviews adoption is deferred to a follow-up plan once the API stabilizes.

Implements: D-21 (composition primitives, NOT monolithic FilterableTable), R7 (pilot-only scope).

<context>
@.planning/phases/19-modular-decomposition/SPEC.md
@.planning/phases/19-modular-decomposition/19-CONTEXT.md
@.planning/phases/19-modular-decomposition/19-RESEARCH.md
@.planning/phases/19-modular-decomposition/19-PATTERNS.md
@services/frontend/app/(app)/posts/page.tsx
@services/frontend/AGENTS.md
@docs/frontend-style.md
@docs/frontend-patterns.md
</context>

<tasks>

<task type="auto">
  <id>19-12-01</id>
  <title>Add DataTable + useDataTableFilters + useDataTableSearch with tests</title>
  <wave>1</wave>
  <read_first>
    - services/frontend/app/(app)/posts/page.tsx (full 567 LOC — extract patterns)
    - services/frontend/hooks/useDebouncedValue.ts (existing — useDataTableSearch reuses)
    - .planning/phases/19-modular-decomposition/19-RESEARCH.md (section 11 "Frontend: DataTable composition" lines 1364-1481)
    - .planning/phases/19-modular-decomposition/19-PATTERNS.md ("Plan 19-12" lines 1207-1314)
  </read_first>
  <action>
    Create three primitive files + their tests.

    1. **`services/frontend/components/lists/DataTable.tsx`** — generic composition primitive:
       ```tsx
       import { type ReactNode } from 'react';
       import { MonoLabel } from '@/components/ui/...';
       // ... preserve existing Linen design system imports from posts/page.tsx

       export interface Column<T> {
         id: string;
         header: ReactNode;
         cell: (row: T) => ReactNode;
         className?: string;
       }

       export interface DataTableProps<T> {
         columns: Column<T>[];
         rows: T[];
         rowKey: (row: T) => string;
         empty?: ReactNode;
         expandable?: (row: T) => ReactNode;  // posts page expands rows; reviews doesn't
         isLoading?: boolean;
         skeleton?: ReactNode;                 // optional custom skeleton
       }

       export function DataTable<T>({
         columns, rows, rowKey, empty, expandable, isLoading, skeleton,
       }: DataTableProps<T>) {
         return (
           <div className="mt-4 overflow-x-auto rounded-md border border-line bg-paper-raised">
             {/* Header row — generated from columns[] */}
             <div className="grid border-b border-line bg-paper-sunken px-5 py-3" style={/* gap and grid-cols from columns */}>
               {columns.map((col) => (
                 <span key={col.id} className={col.className}>
                   <MonoLabel>{col.header}</MonoLabel>
                 </span>
               ))}
             </div>
             {isLoading && (skeleton ?? <DefaultSkeleton />)}
             {!isLoading && rows.length === 0 && (empty ?? null)}
             {!isLoading && rows.map((row) => (
               <DataTableRow
                 key={rowKey(row)}
                 row={row}
                 columns={columns}
                 expandable={expandable}
               />
             ))}
           </div>
         );
       }

       function DataTableRow<T>(/* ... */) { /* row rendering with expand toggle */ }
       function DefaultSkeleton() { /* simple skeleton lines */ }
       ```

       Specific design rules (RESEARCH §11):
       - Generic over `T` — Post for the pilot; later Review.
       - shadcn/ui not adopted here. Preserve current posts page's hand-rolled grid + Linen design system aesthetic.
       - Props interface uses `function` declaration for the component.
       - Do NOT bake filter/search state in. Composition: filters and search live in their own hooks.

    2. **`services/frontend/hooks/useDataTableFilters.ts`**:
       ```ts
       import { useCallback, useState } from 'react';

       export interface UseDataTableFiltersOptions<F> {
         defaultValue: F;
       }

       export function useDataTableFilters<F extends Record<string, string>>(opts: UseDataTableFiltersOptions<F>) {
         const [filters, setFilters] = useState<F>(opts.defaultValue);

         const setFilter = useCallback(<K extends keyof F>(key: K, value: F[K]) => {
           setFilters((prev) => ({ ...prev, [key]: value }));
         }, []);

         const queryString = useCallback(() => {
           const params = new URLSearchParams();
           for (const [k, v] of Object.entries(filters)) {
             if (v !== '' && v !== 'all') {
               params.set(k, v as string);
             }
           }
           return params.toString();
         }, [filters]);

         return { filters, setFilter, queryString };
       }
       ```
       The "skip when value=='all'" semantics matches current `posts/page.tsx:82-84` behaviour exactly.

    3. **`services/frontend/hooks/useDataTableSearch.ts`**:
       ```ts
       import { useMemo, useState } from 'react';
       import { useDebouncedValue } from './useDebouncedValue';

       export interface UseDataTableSearchOptions<T> {
         rows: T[];
         searchableFields: (row: T) => string[];
         debounceMs?: number;
       }

       export function useDataTableSearch<T>(opts: UseDataTableSearchOptions<T>) {
         const [query, setQuery] = useState('');
         const debouncedQuery = useDebouncedValue(query, opts.debounceMs ?? 0);
         const visibleRows = useMemo(() => {
           if (!debouncedQuery.trim()) return opts.rows;
           const q = debouncedQuery.trim().toLowerCase();
           return opts.rows.filter((r) =>
             opts.searchableFields(r).some((s) => s.toLowerCase().includes(q))
           );
         }, [opts.rows, debouncedQuery, opts.searchableFields]);
         return { query, setQuery, visibleRows };
       }
       ```

    4. **Tests** (vitest + @testing-library/react):

       - `services/frontend/components/lists/__tests__/DataTable.test.tsx`:
         - `renders header row from columns config`
         - `renders one DOM row per data row`
         - `shows skeleton when isLoading=true`
         - `shows empty fallback when rows.length===0`
         - `expandable callback rendered when row expanded` (toggle interaction)
         - `rowKey is used as React key` (test by re-rendering with same key, expect stable refs)

       - `services/frontend/hooks/__tests__/useDataTableFilters.test.ts`:
         - `defaultValue is initial state`
         - `setFilter updates one key without affecting others`
         - `queryString skips entries with value === 'all'`
         - `queryString skips entries with empty string`
         - `queryString URL-encodes values`

       - `services/frontend/hooks/__tests__/useDataTableSearch.test.ts`:
         - `returns all rows when query empty`
         - `filters rows by searchableFields output`
         - `case-insensitive match`
         - `debounce delays filter`

    Apply project conventions:
    - `function` declarations (not arrow consts) per frontend AGENTS.md
    - Generic types over `T`/`F`
    - Existing `useDebouncedValue` hook reused (do not re-implement)

    Anti-pattern (D-21 / RESEARCH §11):
    - Do NOT make `<DataTable>` accept filter/search/pagination state — leave that to caller via separate hooks. Composition over monolith.

    Commit subject: `refactor(19): add DataTable + useDataTableFilters + useDataTableSearch primitives`.
  </action>
  <acceptance_criteria>
    - All 6 files exist (3 source + 3 tests)
    - `cd services/frontend && pnpm test --run components/lists/DataTable hooks/useDataTableFilters hooks/useDataTableSearch` exits 0
    - `cd services/frontend && pnpm typecheck` exits 0
    - `rg -c '^export function DataTable\b' services/frontend/components/lists/DataTable.tsx` returns 1
    - `rg -c '^export function useDataTableFilters\b' services/frontend/hooks/useDataTableFilters.ts` returns 1
    - `rg -c '^export function useDataTableSearch\b' services/frontend/hooks/useDataTableSearch.ts` returns 1
    - DataTable does NOT bake in filter state: `rg "useState|filters|setFilter" services/frontend/components/lists/DataTable.tsx | wc -l` returns 0
    - 'all' filtering preserved in queryString: `rg "v !== 'all'|v === 'all'" services/frontend/hooks/useDataTableFilters.ts | wc -l` returns ≥1
    - Reuses existing useDebouncedValue: `rg "useDebouncedValue" services/frontend/hooks/useDataTableSearch.ts | wc -l` returns ≥1
  </acceptance_criteria>
  <automated>cd services/frontend &amp;&amp; pnpm test --run components/lists hooks/useDataTableFilters hooks/useDataTableSearch &amp;&amp; pnpm typecheck</automated>
</task>

<task type="auto">
  <id>19-12-02</id>
  <title>Migrate posts/page.tsx to use DataTable + filter/search hooks (pilot adoption)</title>
  <wave>2</wave>
  <read_first>
    - services/frontend/app/(app)/posts/page.tsx (current 567 LOC — full file)
    - services/frontend/components/lists/DataTable.tsx (created in 19-12-01)
    - services/frontend/hooks/useDataTableFilters.ts (created in 19-12-01)
    - services/frontend/hooks/useDataTableSearch.ts (created in 19-12-01)
    - .planning/phases/19-modular-decomposition/19-PATTERNS.md ("services/frontend/app/(app)/posts/page.tsx (pilot adoption)" lines 1303-1314)
  </read_first>
  <action>
    Refactor `services/frontend/app/(app)/posts/page.tsx` to consume the new primitives:

    1. Replace the `useState` filter slices (current lines ~73-87) with `useDataTableFilters`:
       ```ts
       const { filters, setFilter, queryString } = useDataTableFilters<{
         status: 'all' | 'published' | 'scheduled' | 'error';
         platform: 'all' | 'telegram' | 'vk' | 'yandex_business';
       }>({ defaultValue: { status: 'all', platform: 'all' } });
       ```

    2. Replace the `useQuery` queryKey + URL building with `queryString()`:
       ```ts
       const { data: posts = [], isLoading } = useQuery<Post[]>({
         queryKey: ['posts', filters.status, filters.platform],
         queryFn: () => api.get(`/posts?${queryString()}`).then((r) => r.data.posts ?? []),
       });
       ```

    3. Replace the `visiblePosts` `useMemo` (current ~lines 90-94) with `useDataTableSearch`:
       ```ts
       const { query, setQuery, visibleRows } = useDataTableSearch({
         rows: posts,
         searchableFields: (p) => [p.content],
       });
       ```

    4. Replace the table block (current ~lines 202-260) with `<DataTable>`:
       ```tsx
       const postColumns: Column<Post>[] = [
         { id: 'expand', header: '', cell: () => null, className: 'w-6' },
         { id: 'content', header: 'Контент', cell: (p) => <PostContent content={p.content} /> },
         { id: 'status', header: 'Статус', cell: (p) => <StatusBadge status={p.status} />, className: 'w-[140px]' },
         { id: 'platforms', header: 'Платформы', cell: (p) => <PlatformIcons p={p} />, className: 'w-[200px]' },
         { id: 'date', header: 'Дата', cell: (p) => <DateCell d={p.scheduledAt ?? p.createdAt} />, className: 'w-[160px]' },
         { id: 'actions', header: '', cell: (p) => <PostActions p={p} />, className: 'w-[56px]' },
       ];

       <DataTable<Post>
         columns={postColumns}
         rows={visibleRows}
         rowKey={(p) => p.id}
         expandable={(p) => <PostExpanded p={p} />}
         empty={<PostsEmpty hasFilters={query !== '' || filters.status !== 'all' || filters.platform !== 'all'} />}
         isLoading={isLoading}
       />
       ```

    5. Keep the rest of `posts/page.tsx` intact:
       - PageHeader / stat strip / FilterBar (Select/Tabs/SearchInput) UI unchanged
       - Filter UI now reads from `filters.platform` / `filters.status` and writes via `setFilter`
       - Search input reads `query`, writes `setQuery`
       - All existing modals (CreatePostModal etc.) untouched

    6. After migration, posts/page.tsx should be substantially smaller — target <400 LOC (vs 567 today). Cut count comes from removing inline filter state, queryString building, useMemo searching, and the inline table-block JSX.

    7. Update existing tests at `services/frontend/app/(app)/posts/__tests__/` if they exist (D-16 import-path-only). The full-page integration tests should pass unchanged (component public API and rendered DOM identical).

    Anti-pattern enforcement (RESEARCH §11):
    - Do NOT migrate tasks/page.tsx in this plan (real-time SSE feed)
    - Do NOT migrate integrations/page.tsx in this plan (card grid, not tabular)
    - Reviews adoption deferred — do NOT touch reviews/page.tsx in this plan

    Commit subject: `refactor(19): adopt DataTable on posts/page.tsx (pilot)`.
  </action>
  <acceptance_criteria>
    - `wc -l services/frontend/app/\(app\)/posts/page.tsx | awk '{print $1}'` returns ≤450 (target <400)
    - posts/page.tsx imports the new primitives: `rg "useDataTableFilters|useDataTableSearch|DataTable" services/frontend/app/\(app\)/posts/page.tsx | wc -l` returns ≥3
    - posts/page.tsx no longer uses inline filter useState: `rg "useState<.*'all'|setStatus\(|setPlatform\(" services/frontend/app/\(app\)/posts/page.tsx | wc -l` returns 0
    - Other list pages NOT touched: `git diff $(git merge-base HEAD main)..HEAD -- services/frontend/app/\(app\)/tasks/page.tsx services/frontend/app/\(app\)/integrations/page.tsx services/frontend/app/\(app\)/reviews/page.tsx | wc -l` returns 0
    - `cd services/frontend && pnpm test --run` exits 0
    - `cd services/frontend && pnpm lint && pnpm typecheck` exits 0
  </acceptance_criteria>
  <automated>cd services/frontend &amp;&amp; pnpm test --run &amp;&amp; pnpm typecheck</automated>
</task>

</tasks>

## Verification

```bash
# SC-01 progress: posts page slimmed
test "$(wc -l < services/frontend/app/\(app\)/posts/page.tsx)" -le 450

# Composition (not monolith): DataTable doesn't own filter state
test "$(rg 'useState' services/frontend/components/lists/DataTable.tsx | wc -l)" -eq 0

# Pilot scope honoured: only posts page touched among list pages
git diff --name-only main..HEAD | grep -E '^services/frontend/app/\(app\)/(tasks|integrations|reviews)/page.tsx' | wc -l | awk '$1==0{exit 0}{exit 1}'

# SC-02
cd services/frontend && pnpm test --run && pnpm lint && pnpm typecheck
```

## Success Criteria

- 3 new primitive files (DataTable, useDataTableFilters, useDataTableSearch) with tests
- DataTable is composition primitive — does NOT own filter/search state
- `posts/page.tsx` adopts all three primitives; reduced from 567 LOC to <450 LOC
- `tasks/`, `integrations/`, `reviews/` pages untouched (deferred per RESEARCH §11 + CONTEXT.md)
- All existing tests pass with import-path-only updates (SC-03 / D-16)
- `pnpm test --run && pnpm lint && pnpm typecheck` green
