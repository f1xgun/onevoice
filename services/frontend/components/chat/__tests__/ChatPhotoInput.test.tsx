import { act, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { beforeEach, expect, it, vi } from 'vitest';
import { ChatPhotoInput } from '../ChatPhotoInput';
import { useBusinessStore } from '@/lib/stores/business';
const post = vi.hoisted(() => vi.fn());
vi.mock('@/lib/api/business-api', () => ({ bizApi: () => ({ post }) }));
const attachment = {
  type: 'image',
  url: '/media/photo.png',
  name: 'Photo.png',
  mimeType: 'image/png',
};
beforeEach(() => {
  useBusinessStore.setState({ activeBusinessId: 'business' });
  post.mockReset();
});
function show(onChange: ReturnType<typeof vi.fn>, onUploadingChange = vi.fn()) {
  return render(
    <QueryClientProvider
      client={new QueryClient({ defaultOptions: { mutations: { retry: false } } })}
    >
      <ChatPhotoInput onChange={onChange} onUploadingChange={onUploadingChange} />
    </QueryClientProvider>
  );
}
it('uploads the selected file as multipart and reports pending state separately from the attachment', async () => {
  post.mockResolvedValue({ data: attachment });
  const change = vi.fn(),
    pending = vi.fn();
  show(change, pending);
  const file = new File(['image'], 'Photo.png', { type: 'image/png' });
  fireEvent.change(screen.getByLabelText('Фото'), { target: { files: [file] } });
  await waitFor(() => expect(change).toHaveBeenCalledWith(attachment));
  expect(post.mock.calls[0][0]).toBe('/media');
  expect(post.mock.calls[0][1].get('file')).toBe(file);
  expect(post.mock.calls[0][2]).toEqual({ headers: { 'Content-Type': 'multipart/form-data' } });
  expect(pending.mock.calls.map(([value]) => value)).toEqual([true, false]);
});
it('never attaches a late upload to the next business', async () => {
  let finish!: (value: unknown) => void;
  post.mockReturnValue(
    new Promise((resolve) => {
      finish = resolve;
    })
  );
  const change = vi.fn();
  show(change);
  fireEvent.change(screen.getByLabelText('Фото'), {
    target: { files: [new File(['image'], 'Photo.png', { type: 'image/png' })] },
  });
  await waitFor(() => expect(post).toHaveBeenCalledOnce());
  await act(async () => {
    useBusinessStore.setState({ activeBusinessId: 'other' });
    finish({ data: attachment });
  });
  expect(change).not.toHaveBeenCalled();
});
