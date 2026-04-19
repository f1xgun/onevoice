'use client';

import { useState } from 'react';
import { toast } from 'sonner';
import { api } from '@/lib/api';
import { Button } from '@/components/ui/button';
import { Dialog, DialogContent, DialogHeader, DialogTitle } from '@/components/ui/dialog';
import { Input } from '@/components/ui/input';

interface Props {
  open: boolean;
  onClose: () => void;
}

// VKCommunityModal collects the VK community URL or screen_name and
// redirects the admin to oauth.vk.com to issue a community-scoped token.
// The API resolves screen_name → numeric group_id via the Mini-App
// service key, so the user can paste "vk.com/mycompany", "mycompany",
// or a numeric id interchangeably.
export function VKCommunityModal({ open, onClose }: Props) {
  const [value, setValue] = useState('');
  const [submitting, setSubmitting] = useState(false);

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault();
    const trimmed = value.trim();
    if (!trimmed) return;
    setSubmitting(true);
    try {
      const { data } = await api.get<{ url: string }>(
        `/integrations/vk/community-auth-url?group_id=${encodeURIComponent(trimmed)}`
      );
      window.location.href = data.url;
    } catch (err: unknown) {
      const msg =
        (err as { response?: { data?: { error?: string } } })?.response?.data?.error ||
        'Не удалось подготовить ссылку авторизации';
      toast.error(msg);
      setSubmitting(false);
    }
  }

  function handleClose() {
    setValue('');
    setSubmitting(false);
    onClose();
  }

  return (
    <Dialog open={open} onOpenChange={(v) => !v && handleClose()}>
      <DialogContent className="max-w-lg">
        <DialogHeader>
          <DialogTitle>Подключить сообщество VK</DialogTitle>
        </DialogHeader>

        <form onSubmit={handleSubmit} className="space-y-4">
          <div className="space-y-2">
            <p className="text-sm text-gray-600">
              Вставьте ссылку на сообщество, его короткое имя или числовой ID. Примеры:{' '}
              <span className="font-mono text-xs">vk.com/mycompany</span>,{' '}
              <span className="font-mono text-xs">mycompany</span>,{' '}
              <span className="font-mono text-xs">123456</span>.
            </p>
            <Input
              autoFocus
              value={value}
              onChange={(e) => setValue(e.target.value)}
              placeholder="vk.com/mycompany"
              disabled={submitting}
            />
            <p className="text-xs text-gray-500">
              Вы должны быть администратором сообщества. На следующем шаге VK попросит вас
              подтвердить выдачу прав от имени сообщества.
            </p>
          </div>

          <div className="flex gap-2 pt-2">
            <Button
              type="button"
              variant="outline"
              onClick={handleClose}
              className="flex-1"
              disabled={submitting}
            >
              Отмена
            </Button>
            <Button type="submit" disabled={submitting || !value.trim()} className="flex-1">
              {submitting ? 'Переход в VK...' : 'Подключить'}
            </Button>
          </div>
        </form>
      </DialogContent>
    </Dialog>
  );
}
