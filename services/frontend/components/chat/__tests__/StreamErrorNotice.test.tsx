import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/react';

import { StreamErrorNotice } from '../StreamErrorNotice';

declare const __setTestLocale: (locale: 'ru' | 'en') => void;

describe('StreamErrorNotice', () => {
  it('renders the localized RU message for a known code', () => {
    render(<StreamErrorNotice code="max_iterations" />);
    expect(screen.getByText(/слишком сложным/)).toBeInTheDocument();
  });

  it('renders the localized EN message for a known code', () => {
    __setTestLocale('en');
    render(<StreamErrorNotice code="internal_error" />);
    expect(screen.getByText(/Something went wrong on our side/)).toBeInTheDocument();
  });

  it('renders the generic localized fallback for an unknown code — never [Error: ...]', () => {
    render(
      <StreamErrorNotice
        code={'never_emitted_code' as never}
        detail="max iterations (10) reached"
      />
    );
    expect(
      screen.getByText('Что-то пошло не так. Попробуйте ещё раз чуть позже.')
    ).toBeInTheDocument();
    expect(screen.queryByText(/\[Error:/)).not.toBeInTheDocument();
    const raw = screen.getByText('max iterations (10) reached');
    expect(raw.closest('details')).not.toBeNull();
  });

  it('keeps the raw detail behind a diagnostics affordance, not as the headline', () => {
    render(<StreamErrorNotice code="internal_error" detail="openrouter 500: upstream exploded" />);
    expect(screen.getByText('Подробности')).toBeInTheDocument();
    const detail = screen.getByText('openrouter 500: upstream exploded');
    expect(detail.closest('details')).not.toBeNull();
  });
});
