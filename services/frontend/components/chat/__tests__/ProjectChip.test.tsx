import { describe, expect, it, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import { ProjectChip } from '../ProjectChip';

vi.mock('next/link', () => ({
  default: ({
    children,
    href,
    className,
    ...rest
  }: { children: React.ReactNode; href: string; className?: string } & Record<string, unknown>) => (
    <a href={href} className={className} {...rest}>
      {children}
    </a>
  ),
}));

describe('ProjectChip — Phase 19 / D-05 size variants', () => {
  it('default size (no prop) renders the `sm` Tailwind classes', () => {
    render(<ProjectChip projectId="p-1" projectName="Отзывы" />);
    const link = screen.getByRole('link');
    // sm size: px-2 py-0.5 text-xs gap-1.5
    expect(link.className).toContain('px-2');
    expect(link.className).toContain('text-xs');
  });

  it('size="xs" renders the xs Tailwind classes (mini chip for PinnedSection rows)', () => {
    render(<ProjectChip projectId="p-1" projectName="Отзывы" size="xs" />);
    const link = screen.getByRole('link');
    // xs size: px-1 py-0 text-[10px] gap-1
    expect(link.className).toContain('px-1');
    expect(link.className).toContain('text-[10px]');
  });

  it('size="md" renders the md Tailwind classes', () => {
    render(<ProjectChip projectId="p-1" projectName="Отзывы" size="md" />);
    const link = screen.getByRole('link');
    expect(link.className).toContain('px-3');
    expect(link.className).toContain('text-sm');
  });

  it('renders the unassigned span variant when projectId is null (Без проекта)', () => {
    const { container } = render(<ProjectChip projectId={null} />);
    expect(screen.getByText('Без проекта')).toBeInTheDocument();
    // It's a <span> not a Link.
    expect(container.querySelector('a')).toBeNull();
  });
});
