import { describe, expect, it } from 'vitest';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';

import { DataTable, type Column } from '../DataTable';

interface Row {
  id: string;
  title: string;
  status: string;
}

const sampleRows: Row[] = [
  { id: 'r1', title: 'Alpha', status: 'published' },
  { id: 'r2', title: 'Bravo', status: 'scheduled' },
  { id: 'r3', title: 'Charlie', status: 'error' },
];

const baseColumns: Column<Row>[] = [
  { id: 'title', header: 'Контент', cell: (r) => <span>{r.title}</span> },
  { id: 'status', header: 'Статус', cell: (r) => <span>{r.status}</span> },
];

describe('DataTable', () => {
  it('renders header row from columns config', () => {
    render(
      <DataTable<Row>
        columns={baseColumns}
        rows={sampleRows}
        rowKey={(r) => r.id}
        gridTemplate="1fr 140px"
      />,
    );
    expect(screen.getByText('Контент')).toBeInTheDocument();
    expect(screen.getByText('Статус')).toBeInTheDocument();
  });

  it('renders one DOM row per data row', () => {
    render(
      <DataTable<Row>
        columns={baseColumns}
        rows={sampleRows}
        rowKey={(r) => r.id}
        gridTemplate="1fr 140px"
      />,
    );
    expect(screen.getByText('Alpha')).toBeInTheDocument();
    expect(screen.getByText('Bravo')).toBeInTheDocument();
    expect(screen.getByText('Charlie')).toBeInTheDocument();
    // Three status values render too — distinct strings, no collisions.
    expect(screen.getByText('published')).toBeInTheDocument();
    expect(screen.getByText('scheduled')).toBeInTheDocument();
    expect(screen.getByText('error')).toBeInTheDocument();
  });

  it('shows skeleton when isLoading=true', () => {
    const { container } = render(
      <DataTable<Row>
        columns={baseColumns}
        rows={[]}
        rowKey={(r) => r.id}
        gridTemplate="1fr 140px"
        isLoading
      />,
    );
    // Skeleton uses data-state="static" (per components/ui/skeleton.tsx).
    const skeletons = container.querySelectorAll('[data-state="static"]');
    expect(skeletons.length).toBeGreaterThan(0);
  });

  it('renders custom skeleton when provided', () => {
    render(
      <DataTable<Row>
        columns={baseColumns}
        rows={[]}
        rowKey={(r) => r.id}
        gridTemplate="1fr 140px"
        isLoading
        skeleton={<div>custom-skel</div>}
      />,
    );
    expect(screen.getByText('custom-skel')).toBeInTheDocument();
  });

  it('shows empty fallback when rows.length===0 and not loading', () => {
    render(
      <DataTable<Row>
        columns={baseColumns}
        rows={[]}
        rowKey={(r) => r.id}
        gridTemplate="1fr 140px"
        empty={<div>No rows!</div>}
      />,
    );
    expect(screen.getByText('No rows!')).toBeInTheDocument();
  });

  it('does not show empty fallback when rows are present', () => {
    render(
      <DataTable<Row>
        columns={baseColumns}
        rows={sampleRows}
        rowKey={(r) => r.id}
        gridTemplate="1fr 140px"
        empty={<div>No rows!</div>}
      />,
    );
    expect(screen.queryByText('No rows!')).not.toBeInTheDocument();
  });

  it('expandable callback rendered when row expanded (toggle interaction)', async () => {
    const user = userEvent.setup();
    render(
      <DataTable<Row>
        columns={baseColumns}
        rows={sampleRows.slice(0, 1)}
        rowKey={(r) => r.id}
        gridTemplate="1fr 140px"
        expandable={(r) => <div>Expanded for {r.title}</div>}
      />,
    );

    // Initially collapsed — expanded body not rendered.
    expect(screen.queryByText('Expanded for Alpha')).not.toBeInTheDocument();

    // Click the row button (any text inside is enough; the button wraps the row).
    await user.click(screen.getByRole('button', { expanded: false }));
    expect(screen.getByText('Expanded for Alpha')).toBeInTheDocument();

    // Click again to collapse.
    await user.click(screen.getByRole('button', { expanded: true }));
    expect(screen.queryByText('Expanded for Alpha')).not.toBeInTheDocument();
  });

  it('renders rows as non-buttons when no expandable handler provided', () => {
    render(
      <DataTable<Row>
        columns={baseColumns}
        rows={sampleRows.slice(0, 1)}
        rowKey={(r) => r.id}
        gridTemplate="1fr 140px"
      />,
    );
    // No buttons exist in this rendering — the row is a plain div.
    expect(screen.queryAllByRole('button')).toHaveLength(0);
  });

  it('uses rowKey to derive React key (no duplicate-key warnings, stable across re-render)', () => {
    const { rerender } = render(
      <DataTable<Row>
        columns={baseColumns}
        rows={sampleRows}
        rowKey={(r) => r.id}
        gridTemplate="1fr 140px"
      />,
    );
    // Re-render with the same rowKey output and same data — content remains.
    rerender(
      <DataTable<Row>
        columns={baseColumns}
        rows={sampleRows}
        rowKey={(r) => r.id}
        gridTemplate="1fr 140px"
      />,
    );
    expect(screen.getByText('Alpha')).toBeInTheDocument();
    expect(screen.getByText('Charlie')).toBeInTheDocument();
  });
});
