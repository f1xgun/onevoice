import { act, cleanup, render, screen } from '@testing-library/react';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';

import { LoadingPlaceholder } from './LoadingPlaceholder';

beforeEach(() => vi.useFakeTimers());
afterEach(() => {
  cleanup();
  vi.useRealTimers();
});

it('never reveals a placeholder for a fast request and shows ready content immediately', () => {
  const { rerender } = render(
    <LoadingPlaceholder data-testid="loading">Loading</LoadingPlaceholder>
  );
  expect(screen.getByTestId('loading')).toHaveClass('invisible');
  expect(screen.getByTestId('loading')).toHaveAttribute('aria-hidden', 'true');
  act(() => vi.advanceTimersByTime(100));
  rerender(<p>Ready content</p>);
  expect(screen.getByText('Ready content')).toBeVisible();
  expect(screen.queryByTestId('loading')).not.toBeInTheDocument();
  expect(vi.getTimerCount()).toBe(0);
});

it('reveals slow loading after 200ms and releases ready content without a minimum wait', () => {
  const { rerender } = render(
    <LoadingPlaceholder data-testid="loading">Loading</LoadingPlaceholder>
  );
  act(() => vi.advanceTimersByTime(199));
  expect(screen.getByTestId('loading')).toHaveClass('invisible');
  act(() => vi.advanceTimersByTime(1));
  expect(screen.getByTestId('loading')).not.toHaveClass('invisible');
  expect(screen.getByTestId('loading')).not.toHaveAttribute('aria-hidden');
  rerender(<p>Ready content</p>);
  expect(screen.getByText('Ready content')).toBeVisible();
});

it('cancels the previous loading cycle and does not defer errors', () => {
  const { rerender } = render(
    <LoadingPlaceholder data-testid="loading">Loading</LoadingPlaceholder>
  );
  act(() => vi.advanceTimersByTime(150));
  rerender(<p role="alert">Failed to load</p>);
  expect(screen.getByRole('alert')).toBeVisible();
  rerender(<LoadingPlaceholder data-testid="loading">Retrying</LoadingPlaceholder>);
  act(() => vi.advanceTimersByTime(100));
  expect(screen.getByTestId('loading')).toHaveClass('invisible');
  act(() => vi.advanceTimersByTime(100));
  expect(screen.getByTestId('loading')).not.toHaveClass('invisible');
});
