import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest';
import { render, act, cleanup } from '@testing-library/react';
import { useHighlightMessage } from '@/hooks/useHighlightMessage';

// next/navigation — controllable via mutable state.
const replaceMock = vi.fn();
const params = new URLSearchParams();
let pathnameValue = '/chat/conv-42';

vi.mock('next/navigation', () => ({
  useSearchParams: () => params,
  usePathname: () => pathnameValue,
  useRouter: () => ({ push: vi.fn(), back: vi.fn(), replace: replaceMock }),
}));

function setHighlight(value: string | null) {
  for (const k of Array.from(params.keys())) params.delete(k);
  if (value) params.set('highlight', value);
}

// Mini integration harness: simulates the chat page rendering MessageBubble
// elements with `data-message-id` after messages "load". Mounts
// useHighlightMessage with a controllable readiness flag.
function HighlightFlowHarness({
  messagesReady,
  messageIds,
}: {
  messagesReady: boolean;
  messageIds: string[];
}) {
  useHighlightMessage(messagesReady);
  return (
    <div>
      {messageIds.map((id) => (
        <div key={id} data-message-id={id} data-testid={`bubble-${id}`}>
          message {id}
        </div>
      ))}
    </div>
  );
}

describe('highlight-flow integration — Phase 19 / D-08 / SEARCH-04', () => {
  beforeEach(() => {
    replaceMock.mockReset();
    setHighlight(null);
    pathnameValue = '/chat/conv-42';
  });

  afterEach(() => {
    cleanup();
    vi.useRealTimers();
  });

  it('full flow: ?highlight=msg-1 with messagesReady=true → scrollIntoView + flash + 1750 ms cleanup', () => {
    vi.useFakeTimers();
    setHighlight('msg-1');

    // Spy on Element.prototype.scrollIntoView (set on jsdom by vitest.setup.ts as a no-op).
    const scrollSpy = vi.spyOn(HTMLElement.prototype, 'scrollIntoView').mockImplementation(() => {});

    const { getByTestId } = render(
      <HighlightFlowHarness messagesReady={true} messageIds={['msg-0', 'msg-1', 'msg-2']} />
    );

    const target = getByTestId('bubble-msg-1');
    expect(scrollSpy).toHaveBeenCalledTimes(1);
    expect(target.getAttribute('data-highlight')).toBe('true');

    act(() => {
      vi.advanceTimersByTime(1750);
    });

    expect(target.getAttribute('data-highlight')).toBeNull();
    expect(replaceMock).toHaveBeenCalledWith('/chat/conv-42', { scroll: false });

    scrollSpy.mockRestore();
  });

  it('does not fire when messagesReady=false — even with ?highlight set', () => {
    setHighlight('msg-1');
    const scrollSpy = vi.spyOn(HTMLElement.prototype, 'scrollIntoView').mockImplementation(() => {});

    render(<HighlightFlowHarness messagesReady={false} messageIds={['msg-1']} />);

    expect(scrollSpy).not.toHaveBeenCalled();
    expect(replaceMock).not.toHaveBeenCalled();
    scrollSpy.mockRestore();
  });

  it('silently bails when ?highlight target not in DOM', () => {
    setHighlight('not-mounted');
    const scrollSpy = vi.spyOn(HTMLElement.prototype, 'scrollIntoView').mockImplementation(() => {});

    expect(() =>
      render(<HighlightFlowHarness messagesReady={true} messageIds={['msg-1']} />)
    ).not.toThrow();
    expect(scrollSpy).not.toHaveBeenCalled();
    expect(replaceMock).not.toHaveBeenCalled();
    scrollSpy.mockRestore();
  });
});
