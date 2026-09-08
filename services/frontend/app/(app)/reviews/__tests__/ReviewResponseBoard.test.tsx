import { fireEvent, render, screen } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import {
  parseReviewSLAResponse,
  ReviewResponseBoard,
  type ReviewSLAResponse,
} from '../_components/ReviewResponseBoard';

const populated: ReviewSLAResponse = {
  total: 8,
  unanswered: 4,
  answered: 4,
  buckets: { lt24h: 1, h24to72: 2, gt72h: 1 },
  targetHours: 24,
  medianResponseHours: 5.5,
  averageResponseHours: 8,
  measuredResponses: 3,
  percentAnsweredWithinTarget: 0.67,
  oldestUnansweredHours: 90.5,
  platforms: [
    { platform: 'google', medianResponseHours: 4, measuredResponses: 2 },
    { platform: 'telegram', medianResponseHours: 9.5, measuredResponses: 1 },
  ],
};

function renderBoard(overrides: Partial<React.ComponentProps<typeof ReviewResponseBoard>> = {}) {
  const onRetry = vi.fn();
  render(
    <ReviewResponseBoard
      data={populated}
      isLoading={false}
      isError={false}
      onRetry={onRetry}
      platformLabel={(platform) => (platform === 'google' ? 'Google' : platform)}
      {...overrides}
    />
  );
  return onRetry;
}

describe('ReviewResponseBoard', () => {
  it('rejects a review-list payload returned for the SLA endpoint', () => {
    expect(() => parseReviewSLAResponse([])).toThrow();
  });

  it.each([
    ['missing top-level metric', { ...populated, measuredResponses: undefined }],
    ['non-finite duration', { ...populated, medianResponseHours: Number.NaN }],
    ['negative count', { ...populated, unanswered: -1 }],
    ['fractional count', { ...populated, buckets: { ...populated.buckets, lt24h: 0.5 } }],
    [
      'invalid platform metric',
      {
        ...populated,
        platforms: [{ platform: 'google', medianResponseHours: -1, measuredResponses: 1 }],
      },
    ],
  ])('rejects %s', (_case, payload) => {
    expect(() => parseReviewSLAResponse(payload)).toThrow();
  });

  it('renders full-business age bands, honest median, oldest age and platform medians', () => {
    renderBoard();
    const greenValue = screen.getByText('Без ответа менее 24 ч').nextSibling;
    const amberValue = screen.getByText('Без ответа 24–72 ч').nextSibling;
    const redValue = screen.getByText('Без ответа 72 ч и более').nextSibling;
    expect(greenValue).toHaveTextContent('1');
    expect(greenValue).toHaveClass('text-success');
    expect(amberValue).toHaveTextContent('2');
    expect(amberValue).toHaveClass('text-warning');
    expect(redValue).toHaveTextContent('1');
    expect(redValue).toHaveClass('text-danger');
    expect(screen.getByText('90.5 ч')).toBeInTheDocument();
    expect(screen.getByText('5.5 ч')).toBeInTheDocument();
    expect(screen.getByText('Google')).toBeInTheDocument();
    expect(screen.getByText('9.5 ч')).toBeInTheDocument();
  });

  it('distinguishes no measurements and no unanswered reviews from a zero-hour median', () => {
    renderBoard({
      data: {
        ...populated,
        unanswered: 0,
        buckets: { lt24h: 0, h24to72: 0, gt72h: 0 },
        measuredResponses: 0,
        medianResponseHours: 0,
        oldestUnansweredHours: null,
        platforms: [],
      },
    });
    expect(screen.getByText('Ответов с точным временем пока нет')).toBeInTheDocument();
    expect(screen.getByText('Нет')).toBeInTheDocument();
    expect(screen.queryByText('0 ч')).not.toBeInTheDocument();
    expect(screen.getByText('Без ответа менее 24 ч').nextSibling).toHaveClass('text-ink');
    expect(screen.getByText('Без ответа 24–72 ч').nextSibling).toHaveClass('text-ink');
    expect(screen.getByText('Без ответа 72 ч и более').nextSibling).toHaveClass('text-ink');
  });

  it('renders loading without stale data', () => {
    renderBoard({ isLoading: true });
    expect(screen.getByTestId('response-board-loading')).toBeInTheDocument();
    expect(screen.queryByTestId('response-board-content')).not.toBeInTheDocument();
  });

  it('renders a retryable fetch error', () => {
    const onRetry = renderBoard({ isError: true });
    fireEvent.click(screen.getByRole('button', { name: 'Повторить' }));
    expect(onRetry).toHaveBeenCalledOnce();
    expect(screen.getByRole('alert')).toHaveTextContent(
      'Не удалось загрузить показатели скорости ответа.'
    );
  });
});
