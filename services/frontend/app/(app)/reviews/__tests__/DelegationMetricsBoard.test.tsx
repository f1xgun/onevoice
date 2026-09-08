import { fireEvent, render, screen } from '@testing-library/react';
import {
  DelegationMetricsBoard,
  parseDelegationMetrics,
} from '../_components/DelegationMetricsBoard';

const data = {
  from: '2026-07-20T00:00:00Z',
  to: '2026-09-14T00:00:00Z',
  replied: 4,
  acceptedUnedited: 2,
  edited: 1,
  unknown: 1,
  measurable: 3,
  weeks: [
    { weekStart: '2026-09-07T00:00:00Z', replied: 4, acceptedUnedited: 2, edited: 1, unknown: 1 },
  ],
};

describe('DelegationMetricsBoard', () => {
  it('renders known feedback and legacy unknown separately', () => {
    render(
      <DelegationMetricsBoard data={data} isLoading={false} isError={false} onRetry={() => {}} />
    );
    expect(screen.getByText('2')).toBeInTheDocument();
    expect(screen.getAllByText('1')).toHaveLength(2);
    expect(screen.getByText(/67%/)).toBeInTheDocument();
  });
  it('renders loading', () => {
    render(<DelegationMetricsBoard isLoading isError={false} onRetry={() => {}} />);
    expect(screen.getByTestId('delegation-metrics-loading')).toBeInTheDocument();
  });
  it('renders error and retries', () => {
    const retry = vi.fn();
    render(<DelegationMetricsBoard isLoading={false} isError onRetry={retry} />);
    fireEvent.click(screen.getByRole('button'));
    expect(retry).toHaveBeenCalledOnce();
  });
  it('renders an honest zero state', () => {
    render(
      <DelegationMetricsBoard
        data={{
          ...data,
          replied: 0,
          acceptedUnedited: 0,
          edited: 0,
          unknown: 0,
          measurable: 0,
          weeks: [],
        }}
        isLoading={false}
        isError={false}
        onRetry={() => {}}
      />
    );
    expect(screen.getByText(/известной историей|known edit/i)).toBeInTheDocument();
  });
  it('rejects malformed responses', () => {
    expect(() => parseDelegationMetrics({ ...data, unknown: -1 })).toThrow();
  });
});
