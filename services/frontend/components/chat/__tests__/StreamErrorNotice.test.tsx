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
    expect(screen.queryByText('max iterations (10) reached')).not.toBeInTheDocument();
  });

  it('never renders raw diagnostic detail', () => {
    render(<StreamErrorNotice code="internal_error" detail="openrouter 500: upstream exploded" />);
    expect(screen.queryByText('Подробности')).not.toBeInTheDocument();
    expect(screen.queryByText('openrouter 500: upstream exploded')).not.toBeInTheDocument();
  });
});
