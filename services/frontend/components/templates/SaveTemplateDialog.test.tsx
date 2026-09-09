import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import type { ReactNode } from 'react';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { SaveTemplateDialog } from './SaveTemplateDialog';

const postMock = vi.fn();
vi.mock('@/lib/api/business-api', () => ({
  bizApi: () => ({ post: postMock }),
}));
vi.mock('sonner', () => ({ toast: { success: vi.fn(), error: vi.fn() } }));

function Wrapper({ children }: { children: ReactNode }) {
  return <QueryClientProvider client={new QueryClient()}>{children}</QueryClientProvider>;
}

describe('SaveTemplateDialog', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    postMock.mockResolvedValue({ data: {} });
  });

  it('labels the name and posts the captured source route', async () => {
    const user = userEvent.setup();
    render(<SaveTemplateDialog businessId="business-a" source="post" sourceId="post-a" />, {
      wrapper: Wrapper,
    });
    const opener = screen.getByRole('button', { name: 'Сохранить как шаблон' });
    expect(opener).toHaveClass('min-h-11');
    await user.click(opener);
    expect(screen.getByText(/сотрудники этой организации/)).toBeInTheDocument();
    const name = screen.getByRole('textbox', { name: 'Название шаблона' });
    await user.type(name, 'Публикация недели');
    await user.click(screen.getByRole('button', { name: 'Сохранить' }));
    await waitFor(() =>
      expect(postMock).toHaveBeenCalledWith('/content-templates/from-post/post-a', {
        name: 'Публикация недели',
      })
    );
  });

  it('closes and clears the source dialog when its source changes', async () => {
    const user = userEvent.setup();
    const view = render(
      <SaveTemplateDialog businessId="business-a" source="review" sourceId="review-a" />,
      { wrapper: Wrapper }
    );
    await user.click(screen.getByRole('button', { name: 'Сохранить как шаблон' }));
    await user.type(screen.getByRole('textbox', { name: 'Название шаблона' }), 'Старый источник');
    view.rerender(
      <SaveTemplateDialog businessId="business-a" source="review" sourceId="review-b" />
    );
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument());
    await user.click(screen.getByRole('button', { name: 'Сохранить как шаблон' }));
    expect(screen.getByRole('textbox', { name: 'Название шаблона' })).toHaveValue('');
  });
});
