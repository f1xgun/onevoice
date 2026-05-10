// components/lists/DataTable.tsx — OneVoice (Linen) generic list primitive.
//
// Phase 19 / D-21 — composition primitive. NOT a monolithic FilterableTable.
// Filter / search state lives in sibling hooks (useDataTableFilters,
// useDataTableSearch). This component only owns rendering: header row,
// scroll-x wrapper, skeleton fallback, empty fallback, and optional row
// expand. Columns are typed via a generic <T> and described via
// `Column<T>[]` — each column owns its own header, cell renderer, and
// optional grid-column className.
//
// Hand-rolled grid (no shadcn Table) on purpose: matches the existing
// posts/page.tsx aesthetic (Linen design system), gives callers full
// control over column widths via Tailwind grid-cols-[…] in `gridTemplate`.
//
// Anti-pattern (D-21): do NOT bake filter/search/pagination state in.
// Composition over monolith.
'use client';
import { Fragment, useState, type ReactNode } from 'react';

import { MonoLabel } from '@/components/ui/mono-label';
import { Skeleton } from '@/components/ui/skeleton';

export interface CellContext {
  /** True iff the row is currently expanded (only meaningful when DataTable's `expandable` prop is set). */
  expanded: boolean;
}

export interface Column<T> {
  id: string;
  header: ReactNode;
  /**
   * Cell renderer. Receives the row and (optionally) a context object whose
   * `expanded` flag mirrors the row's expand state — useful when a column
   * needs to swap a chevron-right for a chevron-down based on expansion.
   */
  cell: (row: T, ctx: CellContext) => ReactNode;
  className?: string;
}

export interface DataTableProps<T> {
  columns: Column<T>[];
  rows: T[];
  rowKey: (row: T) => string;
  /** Tailwind value for `grid-template-columns` — e.g. "24px 1fr 140px 56px". */
  gridTemplate: string;
  /** Tailwind value for the `min-width` of header/row grids — e.g. "620px". */
  minWidth?: string;
  empty?: ReactNode;
  expandable?: (row: T) => ReactNode;
  isLoading?: boolean;
  skeleton?: ReactNode;
  /** Class for each row's grid (defaults reasonable for the posts-style aesthetic). */
  rowClassName?: string;
  /** Class applied to the outer container. Defaults to Linen-style border + paper-raised. */
  containerClassName?: string;
}

const DEFAULT_CONTAINER_CLASS =
  'mt-4 overflow-x-auto rounded-md border border-line bg-paper-raised';
const DEFAULT_HEADER_CLASS = 'grid gap-4 border-b border-line bg-paper-sunken px-5 py-3';

export function DataTable<T>({
  columns,
  rows,
  rowKey,
  gridTemplate,
  minWidth,
  empty,
  expandable,
  isLoading,
  skeleton,
  rowClassName,
  containerClassName,
}: DataTableProps<T>) {
  const headerStyle = {
    gridTemplateColumns: gridTemplate,
    minWidth,
  };

  return (
    <div className={containerClassName ?? DEFAULT_CONTAINER_CLASS}>
      <div className={DEFAULT_HEADER_CLASS} style={headerStyle}>
        {columns.map((col) => (
          <span key={col.id} className={col.className}>
            {typeof col.header === 'string' ? <MonoLabel>{col.header}</MonoLabel> : col.header}
          </span>
        ))}
      </div>

      {isLoading
        ? (skeleton ?? (
            <DefaultSkeleton columns={columns} gridTemplate={gridTemplate} minWidth={minWidth} />
          ))
        : rows.length === 0
          ? (empty ?? null)
          : rows.map((row, i) => (
              <DataTableRow
                key={rowKey(row)}
                row={row}
                columns={columns}
                expandable={expandable}
                gridTemplate={gridTemplate}
                minWidth={minWidth}
                isLast={i === rows.length - 1}
                rowClassName={rowClassName}
              />
            ))}
    </div>
  );
}

interface DataTableRowProps<T> {
  row: T;
  columns: Column<T>[];
  expandable?: (row: T) => ReactNode;
  gridTemplate: string;
  minWidth?: string;
  isLast: boolean;
  rowClassName?: string;
}

function DataTableRow<T>({
  row,
  columns,
  expandable,
  gridTemplate,
  minWidth,
  isLast,
  rowClassName,
}: DataTableRowProps<T>) {
  const [expanded, setExpanded] = useState(false);
  const isExpandable = Boolean(expandable);
  const rowStyle = { gridTemplateColumns: gridTemplate, minWidth };

  const baseRowClass =
    rowClassName ??
    'hover:bg-paper-sunken/60 grid w-full items-center gap-4 px-5 py-3.5 text-left transition-colors';
  const containerClass = isLast ? '' : 'border-b border-line-soft';

  const cellCtx: CellContext = { expanded };
  const rowBody = columns.map((col) => (
    <span key={col.id} className={col.className}>
      {col.cell(row, cellCtx)}
    </span>
  ));

  return (
    <div className={containerClass}>
      {isExpandable ? (
        <button
          type="button"
          onClick={() => setExpanded((v) => !v)}
          aria-expanded={expanded}
          className={baseRowClass}
          style={rowStyle}
        >
          {rowBody.map((node, idx) => (
            <Fragment key={columns[idx].id}>{node}</Fragment>
          ))}
        </button>
      ) : (
        <div className={baseRowClass} style={rowStyle}>
          {rowBody}
        </div>
      )}
      {isExpandable && expanded ? expandable!(row) : null}
    </div>
  );
}

interface DefaultSkeletonProps<T> {
  columns: Column<T>[];
  gridTemplate: string;
  minWidth?: string;
}

const SKELETON_ROW_COUNT = 5;

function DefaultSkeleton<T>({ columns, gridTemplate, minWidth }: DefaultSkeletonProps<T>) {
  const rowStyle = { gridTemplateColumns: gridTemplate, minWidth };
  return (
    <div className="divide-y divide-line-soft">
      {Array.from({ length: SKELETON_ROW_COUNT }, (_, i) => (
        <div key={i} className="grid items-center gap-4 px-5 py-4" style={rowStyle} aria-hidden>
          {columns.map((col) => (
            <Skeleton key={col.id} className="h-4 w-3/4" />
          ))}
        </div>
      ))}
    </div>
  );
}
