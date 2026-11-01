import { describe, expect, it } from 'vitest';
import { render, screen } from '@testing-library/react';
import SettingsLayout from '../layout';

describe('settings shell', () => {
  it('renders page content without a duplicate settings navigation', () => {
    render(
      <SettingsLayout>
        <h1>Settings content</h1>
      </SettingsLayout>
    );
    expect(screen.getByRole('heading', { name: 'Settings content' })).toBeInTheDocument();
    expect(screen.queryByRole('navigation')).not.toBeInTheDocument();
  });
});
