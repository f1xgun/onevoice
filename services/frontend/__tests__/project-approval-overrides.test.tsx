import { describe, it, expect, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { ProjectApprovalOverrides } from '@/components/projects/ProjectApprovalOverrides';
import type { Tool, ToolApprovalValue } from '@/lib/schemas';

const telegramPost: Tool = {
  name: 'telegram__send_channel_post',
  platform: 'telegram',
  floor: 'manual',
  editableFields: ['text'],
  description: 'Send a text post to a Telegram channel',
};

describe('ProjectApprovalOverrides — inherit-as-absence (Overview invariant #8)', () => {
  it('renders Inherit as the active selection when value lacks the tool key', () => {
    render(
      <ProjectApprovalOverrides
        tools={[telegramPost]}
        businessApprovals={{ [telegramPost.name]: 'auto' }}
        value={{}}
        onChange={() => {}}
      />
    );

    const inherit = screen.getByLabelText('«как у бизнеса»');
    expect(inherit).toHaveAttribute('data-state', 'checked');

    // Business-default chip must render the business policy explicitly.
    expect(screen.getByText(/Бизнес:\s*Автоматически/)).toBeInTheDocument();
  });

  it('shows «Бизнес: Вручную» when the business default is manual', () => {
    render(
      <ProjectApprovalOverrides
        tools={[telegramPost]}
        businessApprovals={{ [telegramPost.name]: 'manual' }}
        value={{}}
        onChange={() => {}}
      />
    );

    expect(screen.getByText(/Бизнес:\s*Вручную/)).toBeInTheDocument();
  });

  it('calls onChange with the tool set to "manual" when user clicks Manual', async () => {
    const onChange = vi.fn<(next: Record<string, ToolApprovalValue>) => void>();
    render(
      <ProjectApprovalOverrides
        tools={[telegramPost]}
        businessApprovals={{ [telegramPost.name]: 'auto' }}
        value={{}}
        onChange={onChange}
      />
    );

    await userEvent.click(screen.getByLabelText('Вручную'));
    expect(onChange).toHaveBeenCalledWith({ [telegramPost.name]: 'manual' });
  });

  it('calls onChange with the tool set to "auto" when user clicks Auto', async () => {
    const onChange = vi.fn<(next: Record<string, ToolApprovalValue>) => void>();
    render(
      <ProjectApprovalOverrides
        tools={[telegramPost]}
        businessApprovals={{ [telegramPost.name]: 'manual' }}
        value={{}}
        onChange={onChange}
      />
    );

    await userEvent.click(screen.getByLabelText('Автоматически'));
    expect(onChange).toHaveBeenCalledWith({ [telegramPost.name]: 'auto' });
  });

  // CRITICAL: inherit must be encoded as KEY ABSENCE in the outgoing PUT
  // body, never as the string "inherit". Phase 16-07 strips "inherit" on
  // the backend but the frontend contract (Overview invariant #8) is
  // stricter: the frontend must never produce an inherit key at all.
  it('clicking Inherit DELETES the tool key — inherit selection results in a PUT body where the tool key is ABSENT from approvalOverrides', async () => {
    const onChange = vi.fn<(next: Record<string, ToolApprovalValue>) => void>();
    render(
      <ProjectApprovalOverrides
        tools={[telegramPost]}
        businessApprovals={{ [telegramPost.name]: 'auto' }}
        value={{ [telegramPost.name]: 'manual' }}
        onChange={onChange}
      />
    );

    await userEvent.click(screen.getByLabelText('«как у бизнеса»'));

    // Empty map — the tool key MUST NOT be present. NOT {tool: "inherit"}.
    expect(onChange).toHaveBeenCalledWith({});
    const received = onChange.mock.calls[0]?.[0] ?? {};
    expect(Object.keys(received)).not.toContain(telegramPost.name);
    expect(Object.values(received)).not.toContain('inherit');
  });

  it('preserves other entries when toggling one tool to inherit', async () => {
    const vkPost: Tool = {
      name: 'vk__publish_post',
      platform: 'vk',
      floor: 'manual',
      editableFields: ['text'],
      description: '',
    };
    const onChange = vi.fn<(next: Record<string, ToolApprovalValue>) => void>();
    render(
      <ProjectApprovalOverrides
        tools={[telegramPost, vkPost]}
        businessApprovals={{}}
        value={{ [telegramPost.name]: 'auto', [vkPost.name]: 'manual' }}
        onChange={onChange}
      />
    );

    await userEvent.click(screen.getByLabelText(/«как у бизнеса»/i, { selector: '[id^="po-inherit-telegram"]' }));
    expect(onChange).toHaveBeenCalledWith({ [vkPost.name]: 'manual' });
  });

  it('renders nothing meaningful when no manual-floor tools exist', () => {
    render(
      <ProjectApprovalOverrides
        tools={[
          {
            name: 'vk__read_comments',
            platform: 'vk',
            floor: 'auto',
            editableFields: [],
            description: '',
          },
        ]}
        businessApprovals={{}}
        value={{}}
        onChange={() => {}}
      />
    );

    expect(
      screen.getByText(/Нет инструментов, требующих одобрения/)
    ).toBeInTheDocument();
  });
});
