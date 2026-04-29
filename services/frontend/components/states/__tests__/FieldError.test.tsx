import { describe, expect, it } from 'vitest';
import { render, screen } from '@testing-library/react';

import { FieldError } from '@/components/ui/field-error';

describe('FieldError', () => {
  it('renders nothing when no children are passed', () => {
    const { container } = render(<FieldError />);
    expect(container.firstChild).toBeNull();
  });

  it('renders message with role="alert" and danger token color', () => {
    render(<FieldError>Этот ID не распознан.</FieldError>);
    const alert = screen.getByRole('alert');
    expect(alert).toHaveTextContent('Этот ID не распознан.');
    expect(alert.className).toMatch(/text-\[var\(--ov-danger\)\]/);
  });

  it('omits the leading dot when hideDot is true', () => {
    const { container } = render(<FieldError hideDot>Этот ID не распознан.</FieldError>);
    // The dot is the only aria-hidden span inside the alert; without it,
    // the alert contains a single text-only <span>.
    const dot = container.querySelector('span[aria-hidden="true"]');
    expect(dot).toBeNull();
  });
});
