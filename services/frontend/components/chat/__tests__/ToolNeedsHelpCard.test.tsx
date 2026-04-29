import { describe, expect, it, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';

import { ToolNeedsHelpCard } from '../ToolNeedsHelpCard';

describe('ToolNeedsHelpCard', () => {
  it('renders mono tool name, message, and the "нужна помощь" pill', () => {
    render(
      <ToolNeedsHelpCard
        toolName="review.draft_reply"
        message="Не уверена, как ответить на этот отзыв."
        onHelp={() => undefined}
      />
    );
    expect(screen.getByText('review.draft_reply')).toBeInTheDocument();
    expect(screen.getByText(/Не уверена, как ответить/)).toBeInTheDocument();
    expect(screen.getByText('нужна помощь')).toBeInTheDocument();
  });

  it('invokes onHelp when the primary button is clicked', async () => {
    const onHelp = vi.fn();
    const user = userEvent.setup();
    render(
      <ToolNeedsHelpCard
        toolName="review.draft_reply"
        message="Помощь"
        onHelp={onHelp}
      />
    );
    await user.click(screen.getByRole('button', { name: 'Помочь' }));
    expect(onHelp).toHaveBeenCalledTimes(1);
  });

  it('renders the secondary "Дать контекст" button only when onProvideContext is supplied', async () => {
    const onProvideContext = vi.fn();
    const user = userEvent.setup();
    const { rerender } = render(
      <ToolNeedsHelpCard
        toolName="review.draft_reply"
        message="."
        onHelp={() => undefined}
      />
    );
    expect(
      screen.queryByRole('button', { name: 'Дать контекст' })
    ).not.toBeInTheDocument();

    rerender(
      <ToolNeedsHelpCard
        toolName="review.draft_reply"
        message="."
        onHelp={() => undefined}
        onProvideContext={onProvideContext}
      />
    );
    await user.click(screen.getByRole('button', { name: 'Дать контекст' }));
    expect(onProvideContext).toHaveBeenCalledTimes(1);
  });
});
