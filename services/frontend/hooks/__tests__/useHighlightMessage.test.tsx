import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest';
import { render, act, cleanup } from '@testing-library/react';
import { useHighlightMessage } from '@/hooks/useHighlightMessage';

// Controllable next/navigation mocks.
const replaceMock = vi.fn();
const params = new URLSearchParams();
let pathnameValue = '/chat/conv-1';

vi.mock('next/navigation', () => ({
  useSearchParams: () => params,
  usePathname: () => pathnameValue,
  useRouter: () => ({
    push: vi.fn(),
    back: vi.fn(),
    replace: replaceMock,
  }),
}));

// Test harness component that mounts the hook.
function Harness({ messagesReady }: { messagesReady: boolean }) {
  useHighlightMessage(messagesReady);
  return null;
}

function setHighlightParam(value: string | null) {
  // Reset the same URLSearchParams instance so the hook's `params` reference is stable.
  // Removing all keys keeps the same object identity, which matches Next's behavior of
  // returning the same ReadonlyURLSearchParams unless the URL changes.
  for (const k of Array.from(params.keys())) params.delete(k);
  if (value !== null) params.set('highlight', value);
}

describe('useHighlightMessage — Phase 19 / D-08 / SEARCH-04', () => {
  beforeEach(() => {
    replaceMock.mockReset();
    setHighlightParam(null);
    pathnameValue = '/chat/conv-1';
  });

  afterEach(() => {
    cleanup();
    vi.useRealTimers();
  });

  it('does nothing when messagesReady=false (waits for messages to mount)', () => {
    setHighlightParam('msg-1');
    const target = document.createElement('div');
    target.setAttribute('data-message-id', 'msg-1');
    document.body.appendChild(target);
    const scrollSpy = vi.fn();
    target.scrollIntoView = scrollSpy;

    render(<Harness messagesReady={false} />);

    expect(scrollSpy).not.toHaveBeenCalled();
    expect(target.getAttribute('data-highlight')).toBeNull();
    document.body.removeChild(target);
  });

  it('does nothing when ?highlight is absent', () => {
    setHighlightParam(null);
    const target = document.createElement('div');
    target.setAttribute('data-message-id', 'msg-1');
    document.body.appendChild(target);
    const scrollSpy = vi.fn();
    target.scrollIntoView = scrollSpy;

    render(<Harness messagesReady={true} />);

    expect(scrollSpy).not.toHaveBeenCalled();
    expect(target.getAttribute('data-highlight')).toBeNull();
    document.body.removeChild(target);
  });

  it('scrolls + sets data-highlight=true when target is in DOM', () => {
    setHighlightParam('msg-1');
    const target = document.createElement('div');
    target.setAttribute('data-message-id', 'msg-1');
    document.body.appendChild(target);
    const scrollSpy = vi.fn();
    target.scrollIntoView = scrollSpy;

    render(<Harness messagesReady={true} />);

    expect(scrollSpy).toHaveBeenCalledTimes(1);
    expect(scrollSpy).toHaveBeenCalledWith({ behavior: 'smooth', block: 'center' });
    expect(target.getAttribute('data-highlight')).toBe('true');
    document.body.removeChild(target);
  });

  it('removes data-highlight + calls router.replace(pathname) after 1750 ms', () => {
    vi.useFakeTimers();
    setHighlightParam('msg-1');
    const target = document.createElement('div');
    target.setAttribute('data-message-id', 'msg-1');
    document.body.appendChild(target);
    target.scrollIntoView = vi.fn();

    render(<Harness messagesReady={true} />);
    expect(target.getAttribute('data-highlight')).toBe('true');

    act(() => {
      vi.advanceTimersByTime(1749);
    });
    // Just before the flash window closes — still highlighted, no replace yet.
    expect(target.getAttribute('data-highlight')).toBe('true');
    expect(replaceMock).not.toHaveBeenCalled();

    act(() => {
      vi.advanceTimersByTime(1);
    });
    expect(target.getAttribute('data-highlight')).toBeNull();
    expect(replaceMock).toHaveBeenCalledWith('/chat/conv-1', { scroll: false });
    document.body.removeChild(target);
  });

  it('silently ignores when target is not in DOM', () => {
    setHighlightParam('missing-msg');

    expect(() => render(<Harness messagesReady={true} />)).not.toThrow();
    // No replace because no element matched.
    expect(replaceMock).not.toHaveBeenCalled();
  });

  it('uses CSS.escape so special chars in msgId still match', () => {
    // Inject an msgId containing a chunk that would otherwise be interpreted as
    // a CSS combinator/quote — e.g., the literal `"` and `]` characters.
    const tricky = '5f7c#weird"id';
    setHighlightParam(tricky);
    const target = document.createElement('div');
    target.setAttribute('data-message-id', tricky);
    document.body.appendChild(target);
    target.scrollIntoView = vi.fn();

    expect(() => render(<Harness messagesReady={true} />)).not.toThrow();
    expect(target.getAttribute('data-highlight')).toBe('true');
    document.body.removeChild(target);
  });
});

// Static-content test: globals.css must contain the flash keyframe + reduced-motion fallback.
describe('globals.css — Phase 19 / D-08 flash keyframe', () => {
  it('contains @keyframes onevoice-flash + [data-highlight] + prefers-reduced-motion', async () => {
    const { readFileSync } = await import('fs');
    const path = await import('path');
    const cssPath = path.resolve(__dirname, '../..', 'app', 'globals.css');
    const css = readFileSync(cssPath, 'utf8');
    expect(css).toMatch(/@keyframes\s+onevoice-flash/);
    expect(css).toMatch(/\[data-highlight=['"]true['"]\]/);
    expect(css).toMatch(/prefers-reduced-motion:\s*reduce/);
  });
});
