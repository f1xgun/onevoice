import { describe, expect, it } from 'vitest';
import { render, screen } from '@testing-library/react';

import { ConnectChannelHint, shouldPromptConnectChannel } from '../ConnectChannelHint';

declare const __setTestLocale: (locale: 'ru' | 'en') => void;

describe('shouldPromptConnectChannel', () => {
  // resolved === false covers BOTH the in-flight (placeholder) state AND a
  // failed fetch (the call site passes isSuccess). Both must keep the chips —
  // an errored integrations query must not be read as "no active channel".
  it('returns false when the query has not successfully loaded (in-flight or error)', () => {
    expect(shouldPromptConnectChannel([], false)).toBe(false);
    expect(shouldPromptConnectChannel(undefined, false)).toBe(false);
    expect(shouldPromptConnectChannel([{ status: 'active' }], false)).toBe(false);
  });

  it('returns true when resolved with no active channel', () => {
    expect(shouldPromptConnectChannel([], true)).toBe(true);
    expect(shouldPromptConnectChannel(undefined, true)).toBe(true);
    expect(shouldPromptConnectChannel([{ status: 'token_expired' }], true)).toBe(true);
  });

  it('returns false when resolved with at least one active channel', () => {
    expect(shouldPromptConnectChannel([{ status: 'active' }], true)).toBe(false);
    expect(
      shouldPromptConnectChannel([{ status: 'token_expired' }, { status: 'active' }], true)
    ).toBe(false);
  });
});

describe('ConnectChannelHint', () => {
  it('RU: renders the hint copy and a connect button linking to /integrations', () => {
    render(<ConnectChannelHint />);
    expect(screen.getByText(/Подключите канал/i)).toBeInTheDocument();
    const link = screen.getByRole('link', { name: 'Подключить канал' });
    expect(link).toHaveAttribute('href', '/integrations');
  });

  it('EN: renders the localized hint and connect button', () => {
    __setTestLocale('en');
    render(<ConnectChannelHint />);
    expect(screen.getByText(/Connect a channel so I can/i)).toBeInTheDocument();
    const link = screen.getByRole('link', { name: 'Connect a channel' });
    expect(link).toHaveAttribute('href', '/integrations');
  });
});
