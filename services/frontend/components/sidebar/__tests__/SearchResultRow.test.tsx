import { describe, expect, it, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import { SearchResultRow, renderHighlightedSnippet } from '../SearchResultRow';
import type { SearchResult } from '@/types/search';

vi.mock('next/navigation', () => ({
  useRouter: () => ({ push: vi.fn(), back: vi.fn(), replace: vi.fn() }),
}));

const baseResult: SearchResult = {
  conversationId: 'c-1',
  title: 'My conversation',
  projectId: null,
  snippet: 'hello world this is a snippet',
  matchCount: 1,
  topMessageId: 'msg-1',
  score: 12.3,
  marks: [],
  lastMessageAt: '2026-04-27T12:00:00Z',
};

describe('SearchResultRow', () => {
  it('renders title + snippet + date and links to /chat/{id}?highlight={topMessageId}', () => {
    render(<SearchResultRow result={baseResult} query="hello" />);
    const links = screen.getAllByRole('link');
    expect(links[0]).toHaveAttribute('href', '/chat/c-1?highlight=msg-1');
    expect(screen.getByText('My conversation')).toBeInTheDocument();
    expect(screen.getByText(/hello world/)).toBeInTheDocument();
  });

  it('omits the +N совпадений badge when matchCount == 1 (single match)', () => {
    render(<SearchResultRow result={{ ...baseResult, matchCount: 1 }} query="hello" />);
    expect(screen.queryByText(/совпадений/)).not.toBeInTheDocument();
  });

  it('renders +N совпадений badge when matchCount > 1', () => {
    render(<SearchResultRow result={{ ...baseResult, matchCount: 5 }} query="hello" />);
    expect(screen.getByText(/\+4 совпадений/)).toBeInTheDocument();
  });

  it('renders a ProjectChip with size="xs" when projectId is non-null', () => {
    const { container } = render(
      <SearchResultRow result={{ ...baseResult, projectId: 'p-1' }} query="hello" />
    );
    const icon = container.querySelector('svg');
    expect(icon).not.toBeNull();
    expect(icon?.getAttribute('width')).toBe('10');
  });

  it('does NOT nest chip-link inside row-link when projectId is set (avoids <a in a> hydration warning)', () => {
    const { container } = render(
      <SearchResultRow result={{ ...baseResult, projectId: 'p-1' }} query="hello" />
    );
    const anchors = container.querySelectorAll('a');
    for (const a of Array.from(anchors)) {
      expect(a.querySelector('a')).toBeNull();
    }
  });

  it('falls back to /chat/{id} (no ?highlight) when topMessageId is absent', () => {
    render(<SearchResultRow result={{ ...baseResult, topMessageId: undefined }} query="hello" />);
    const links = screen.getAllByRole('link');
    expect(links[0]).toHaveAttribute('href', '/chat/c-1');
  });

  it('shows fallback "Новый чат" when title is empty', () => {
    render(<SearchResultRow result={{ ...baseResult, title: '' }} query="hello" />);
    expect(screen.getByText('Новый чат')).toBeInTheDocument();
  });
});

describe('renderHighlightedSnippet', () => {
  it('returns the raw string when no marks supplied', () => {
    const out = renderHighlightedSnippet('hello world');
    expect(out).toBe('hello world');
  });

  it('wraps a single byte range in <mark>', () => {
    const { container } = render(<div>{renderHighlightedSnippet('hello world', [[6, 11]])}</div>);
    const mark = container.querySelector('mark');
    expect(mark).not.toBeNull();
    expect(mark?.textContent).toBe('world');
  });

  it('wraps multiple byte ranges and preserves between-text', () => {
    const { container } = render(
      <div>
        {renderHighlightedSnippet('foo bar baz', [
          [0, 3],
          [8, 11],
        ])}
      </div>
    );
    const marks = container.querySelectorAll('mark');
    expect(marks).toHaveLength(2);
    expect(marks[0]?.textContent).toBe('foo');
    expect(marks[1]?.textContent).toBe('baz');
    expect(container.textContent).toBe('foo bar baz');
  });

  it('handles Russian (BMP — byte offsets coincide with JS chars for Cyrillic up to 2-byte UTF-8)', () => {
    const { container } = render(<div>{renderHighlightedSnippet('привет', [[2, 10]])}</div>);
    const mark = container.querySelector('mark');
    expect(mark?.textContent).toBe('риве');
  });
});
