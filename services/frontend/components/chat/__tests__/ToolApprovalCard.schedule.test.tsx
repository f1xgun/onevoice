import { render, screen, fireEvent, waitFor, within } from '@testing-library/react';
import { expect, it, vi } from 'vitest';
import { ToolApprovalCard } from '../ToolApprovalCard';
import { singleCallBatch } from '@/test-utils/pending-approval-fixtures';
import { format } from 'date-fns';
const batch = {
  ...singleCallBatch,
  calls: [
    {
      ...singleCallBatch.calls[0],
      editableFields: ['text', 'scheduled_at'],
      args: { text: 'Keep this text', channel_id: '@owned' },
    },
  ],
};
it('chooses future time as a manual edit containing only scheduled_at', async () => {
  const submit = vi.fn().mockResolvedValue(undefined);
  render(<ToolApprovalCard batch={batch} onSubmit={submit} />);
  fireEvent.click(screen.getByRole('button', { name: /^Когда:/ }));
  const dialog = screen.getByRole('dialog');
  const date = new Date(Date.now() + 86_400_000);
  const local = format(date, "yyyy-MM-dd'T'HH:mm");
  fireEvent.change(within(dialog).getByLabelText('Выбрать день и время'), {
    target: { value: local },
  });
  fireEvent.click(within(dialog).getByRole('button', { name: 'Готово', exact: true }));
  await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument());
  expect(submit).not.toHaveBeenCalled();
  fireEvent.click(screen.getByRole('button', { name: 'Подтвердить', exact: true }));
  await waitFor(() =>
    expect(submit).toHaveBeenCalledWith([
      {
        id: batch.calls[0].callId,
        action: 'edit',
        edited_args: { scheduled_at: new Date(local).toISOString() },
      },
    ])
  );
});
it('Now explicitly clears the time only through a persisted edit decision', async () => {
  const submit = vi.fn().mockResolvedValue(undefined);
  render(<ToolApprovalCard batch={batch} onSubmit={submit} />);
  fireEvent.click(screen.getByRole('button', { name: /^Когда:/ }));
  fireEvent.click(
    within(screen.getByRole('dialog')).getByRole('button', { name: 'Сразу после вашего «да»' })
  );
  fireEvent.click(screen.getByRole('button', { name: 'Подтвердить', exact: true }));
  await waitFor(() =>
    expect(submit).toHaveBeenCalledWith([
      { id: batch.calls[0].callId, action: 'edit', edited_args: { scheduled_at: '' } },
    ])
  );
});
it('rejects invalid or past time without changing the decision or sending', async () => {
  const submit = vi.fn();
  render(<ToolApprovalCard batch={batch} onSubmit={submit} />);
  fireEvent.click(screen.getByRole('button', { name: /^Когда:/ }));
  const dialog = screen.getByRole('dialog');
  fireEvent.change(within(dialog).getByLabelText('Выбрать день и время'), {
    target: { value: '2000-01-01T12:00' },
  });
  fireEvent.click(within(dialog).getByRole('button', { name: 'Готово', exact: true }));
  expect(await within(dialog).findByRole('alert')).toHaveTextContent('в будущем');
  expect(submit).not.toHaveBeenCalled();
});
