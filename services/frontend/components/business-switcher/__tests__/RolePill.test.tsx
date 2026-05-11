import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/react';
import { RolePill } from '../RolePill';

describe('RolePill', () => {
  it('renders the Russian label for the four system roles', () => {
    const { rerender } = render(<RolePill roleName="owner" />);
    expect(screen.getByText('владелец')).toBeInTheDocument();

    rerender(<RolePill roleName="admin" />);
    expect(screen.getByText('админ')).toBeInTheDocument();

    rerender(<RolePill roleName="editor" />);
    expect(screen.getByText('редактор')).toBeInTheDocument();

    rerender(<RolePill roleName="viewer" />);
    expect(screen.getByText('наблюдатель')).toBeInTheDocument();
  });

  it('lowercases an unknown custom role name', () => {
    render(<RolePill roleName="Marketing-Lead" />);
    expect(screen.getByText('marketing-lead')).toBeInTheDocument();
  });

  it('truncates long custom role names and exposes the full value via title attribute', () => {
    // 30 chars — exceeds the 24-char cap
    const longName = 'super-long-marketing-strategist';
    render(<RolePill roleName={longName} />);
    // Expect a span with ellipsis + title attribute carrying the full lowercase string.
    const visible = screen.getByText((content) => content.endsWith('…'));
    expect(visible).toBeInTheDocument();
    expect(visible).toHaveAttribute('title', longName.toLowerCase());
    // The visible string should be 24 chars max (23 + ellipsis).
    expect(visible.textContent?.length).toBeLessThanOrEqual(24);
  });

  it('renders the mono kicker visual tokens without forcing uppercase', () => {
    render(<RolePill roleName="owner" />);
    const pill = screen.getByText('владелец');
    expect(pill.className).toMatch(/font-mono/);
    expect(pill.className).toMatch(/bg-paper-sunken/);
    expect(pill.className).toMatch(/text-ink-soft/);
    // Critical: MonoLabel forces uppercase — RolePill must NOT.
    expect(pill.className).not.toMatch(/\buppercase\b/);
  });
});
