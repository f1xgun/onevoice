import { describe, expect, it } from 'vitest';
import { render, fireEvent } from '@testing-library/react';
import type { RefObject } from 'react';
import { useRovingTabIndex } from '../useRovingTabIndex';

// Phase 19 / Plan 19-05 / D-17 — roving-tabindex behavior contract.
//
// Fixture renders N <button data-roving-item> children inside a single
// container that wires the hook's containerRef + onKeyDown. The hook
// flips tabindex on the items as focus moves; tests assert that flip.

function Fixture({ count }: { count: number }) {
  const { containerRef, onKeyDown } = useRovingTabIndex(count);
  return (
    <div
      ref={containerRef as RefObject<HTMLDivElement>}
      onKeyDown={onKeyDown}
      data-testid="container"
    >
      {Array.from({ length: count }, (_, i) => (
        <button key={i} data-roving-item data-i={i} type="button">
          {`item ${i}`}
        </button>
      ))}
    </div>
  );
}

describe('useRovingTabIndex — Phase 19 / Plan 19-05 / D-17', () => {
  it('initial tabindex distribution is [0, -1, -1] for 3 items', () => {
    const { container } = render(<Fixture count={3} />);
    const items = container.querySelectorAll('[data-roving-item]');
    expect(items[0].getAttribute('tabindex')).toBe('0');
    expect(items[1].getAttribute('tabindex')).toBe('-1');
    expect(items[2].getAttribute('tabindex')).toBe('-1');
  });

  it('ArrowDown moves the tabindex=0 to the next item', () => {
    const { getByTestId, container } = render(<Fixture count={3} />);
    fireEvent.keyDown(getByTestId('container'), { key: 'ArrowDown' });
    const items = container.querySelectorAll('[data-roving-item]');
    expect(items[0].getAttribute('tabindex')).toBe('-1');
    expect(items[1].getAttribute('tabindex')).toBe('0');
    expect(items[2].getAttribute('tabindex')).toBe('-1');
  });

  it('ArrowDown wraps from last item back to first', () => {
    const { getByTestId, container } = render(<Fixture count={3} />);
    // Move to last (index 2) via End so wrap-around is observable.
    fireEvent.keyDown(getByTestId('container'), { key: 'End' });
    fireEvent.keyDown(getByTestId('container'), { key: 'ArrowDown' });
    const items = container.querySelectorAll('[data-roving-item]');
    expect(items[0].getAttribute('tabindex')).toBe('0');
    expect(items[2].getAttribute('tabindex')).toBe('-1');
  });

  it('ArrowUp wraps from first item back to last', () => {
    const { getByTestId, container } = render(<Fixture count={3} />);
    fireEvent.keyDown(getByTestId('container'), { key: 'ArrowUp' });
    const items = container.querySelectorAll('[data-roving-item]');
    expect(items[0].getAttribute('tabindex')).toBe('-1');
    expect(items[2].getAttribute('tabindex')).toBe('0');
  });

  it('Home jumps to first item; End jumps to last item', () => {
    const { getByTestId, container } = render(<Fixture count={4} />);
    fireEvent.keyDown(getByTestId('container'), { key: 'End' });
    let items = container.querySelectorAll('[data-roving-item]');
    expect(items[3].getAttribute('tabindex')).toBe('0');
    expect(items[0].getAttribute('tabindex')).toBe('-1');

    fireEvent.keyDown(getByTestId('container'), { key: 'Home' });
    items = container.querySelectorAll('[data-roving-item]');
    expect(items[0].getAttribute('tabindex')).toBe('0');
    expect(items[3].getAttribute('tabindex')).toBe('-1');
  });

  it('non-navigation keys are NOT preventDefault-ed (Enter falls through)', () => {
    const { getByTestId } = render(<Fixture count={3} />);
    const container = getByTestId('container');
    const event = new KeyboardEvent('keydown', { key: 'Enter', bubbles: true, cancelable: true });
    container.dispatchEvent(event);
    expect(event.defaultPrevented).toBe(false);
  });

  it('itemCount=0 is a no-op (no errors, no tabindex set)', () => {
    const { getByTestId, container } = render(<Fixture count={0} />);
    fireEvent.keyDown(getByTestId('container'), { key: 'ArrowDown' });
    expect(container.querySelectorAll('[data-roving-item]').length).toBe(0);
  });
});
