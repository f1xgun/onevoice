import { describe, expect, it, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import { IntlClientProvider } from '@/components/IntlClientProvider';
import { ToolApprovalToggle } from '@/app/(app)/settings/tools/ToolApprovalToggle';
import en from '@/messages/en.json';
import ru from '@/messages/ru.json';
import catalog from './fixtures/tool-catalog.json';

// Tool names snapshot services/orchestrator/internal/wire/tools*.go registrations.
// Values resolve through pkg/tools/constants.go, independently of translations.

vi.unmock('next-intl');

describe('tool catalog locale', () => {
  it.each(catalog)('localizes the name, description and approval label for $name', (tool) => {
    const { container, rerender } = render(
      <IntlClientProvider locale="en" messages={en}>
        <ToolApprovalToggle tool={{ ...tool, floor: 'manual' }} value="manual" onChange={vi.fn()} />
      </IntlClientProvider>
    );
    expect(container.textContent).not.toMatch(/[А-Яа-яЁё]/);
    const group = screen.getByRole('radiogroup');
    expect(group.getAttribute('aria-label')).not.toMatch(/[А-Яа-яЁё]/);
    expect(container.querySelectorAll('p')).toHaveLength(2);
    rerender(
      <IntlClientProvider locale="ru" messages={ru}>
        <ToolApprovalToggle tool={{ ...tool, floor: 'manual' }} value="manual" onChange={vi.fn()} />
      </IntlClientProvider>
    );
    expect(container.textContent).toMatch(/[А-Яа-яЁё]/);
    expect(screen.queryByText(tool.displayName)).not.toBeInTheDocument();
    expect(screen.queryByText(tool.userDescription)).not.toBeInTheDocument();
  });

  it('retains server copy for an unknown tool', () => {
    render(
      <IntlClientProvider locale="en" messages={en}>
        <ToolApprovalToggle
          tool={{
            name: 'future__action',
            platform: 'telegram',
            floor: 'manual',
            editableFields: [],
            displayName: 'Future action',
            userDescription: 'Future description',
          }}
          value="manual"
          onChange={vi.fn()}
        />
      </IntlClientProvider>
    );
    expect(screen.getByText('Future action')).toBeInTheDocument();
    expect(screen.getByText('Future description')).toBeInTheDocument();
    expect(
      screen.getByRole('radiogroup', { name: 'Approval mode for Future action' })
    ).toBeInTheDocument();
  });
});
