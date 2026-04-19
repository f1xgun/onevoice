'use client';

import { useEffect, useState } from 'react';
import { toast } from 'sonner';
import { api } from '@/lib/api';
import { Button } from '@/components/ui/button';
import { Dialog, DialogContent, DialogHeader, DialogTitle } from '@/components/ui/dialog';

interface Community {
  id: number;
  name: string;
  screen_name: string;
  photo_50: string;
  members_count: number;
}

interface Props {
  open: boolean;
  onClose: () => void;
}

// VKCommunityModal renders the second step of the VK OAuth flow:
// the user has completed VK ID (PKCE) authorization, the API has stored
// their user token in Redis, and now we pick which community to issue a
// community-scoped token for. Selecting a community redirects to the
// second OAuth hop (oauth.vk.com/authorize?group_ids=…) which stores both
// tokens via integrationService.Connect.
export function VKCommunityModal({ open, onClose }: Props) {
  const [communities, setCommunities] = useState<Community[]>([]);
  const [loading, setLoading] = useState(false);
  const [submitting, setSubmitting] = useState(false);
  const [selected, setSelected] = useState<number | null>(null);

  useEffect(() => {
    if (!open) return;
    let cancelled = false;
    setLoading(true);
    api
      .get<Community[]>('/integrations/vk/communities')
      .then(({ data }) => {
        if (cancelled) return;
        setCommunities(Array.isArray(data) ? data : []);
      })
      .catch((err: unknown) => {
        if (cancelled) return;
        const msg =
          (err as { response?: { data?: { error?: string } } })?.response?.data?.error ||
          'Не удалось получить список сообществ. Попробуйте подключить VK заново.';
        toast.error(msg);
        onClose();
      })
      .finally(() => {
        if (!cancelled) setLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, [open, onClose]);

  async function handleSubmit() {
    if (selected == null) return;
    setSubmitting(true);
    try {
      const { data } = await api.get<{ url: string }>(
        `/integrations/vk/community-auth-url?group_id=${selected}`
      );
      window.location.href = data.url;
    } catch (err: unknown) {
      const msg =
        (err as { response?: { data?: { error?: string } } })?.response?.data?.error ||
        'Ошибка получения ссылки авторизации';
      toast.error(msg);
      setSubmitting(false);
    }
  }

  function handleClose() {
    setCommunities([]);
    setSelected(null);
    setSubmitting(false);
    onClose();
  }

  return (
    <Dialog open={open} onOpenChange={(v) => !v && handleClose()}>
      <DialogContent className="max-w-lg">
        <DialogHeader>
          <DialogTitle>Выберите сообщество VK</DialogTitle>
        </DialogHeader>

        {loading ? (
          <div className="flex justify-center py-8">
            <div className="h-6 w-6 animate-spin rounded-full border-2 border-gray-300 border-t-indigo-600" />
          </div>
        ) : communities.length === 0 ? (
          <div className="rounded-lg bg-amber-50 p-3 text-sm text-amber-900">
            Не найдено сообществ, в которых вы администратор. Убедитесь, что роль администратора
            назначена на стороне VK, и попробуйте заново.
          </div>
        ) : (
          <div className="space-y-2">
            <p className="text-sm text-gray-600">
              Публикации и комментарии будут привязаны к выбранному сообществу.
            </p>
            <div className="max-h-80 space-y-1 overflow-y-auto">
              {communities.map((c) => (
                <label
                  key={c.id}
                  className={`flex cursor-pointer items-center gap-3 rounded-lg border p-3 text-sm transition ${
                    selected === c.id
                      ? 'border-indigo-500 bg-indigo-50'
                      : 'border-gray-200 hover:bg-gray-50'
                  }`}
                >
                  <input
                    type="radio"
                    name="vk-community"
                    value={c.id}
                    checked={selected === c.id}
                    onChange={() => setSelected(c.id)}
                    className="shrink-0"
                  />
                  {c.photo_50 && (
                    // eslint-disable-next-line @next/next/no-img-element
                    <img
                      src={c.photo_50}
                      alt=""
                      className="h-10 w-10 shrink-0 rounded"
                      loading="lazy"
                    />
                  )}
                  <div className="min-w-0 flex-1">
                    <div className="truncate font-medium">{c.name}</div>
                    <div className="truncate text-xs text-gray-500">
                      @{c.screen_name} · {c.members_count.toLocaleString('ru-RU')} участников
                    </div>
                  </div>
                </label>
              ))}
            </div>
          </div>
        )}

        <div className="flex gap-2 pt-2">
          <Button variant="outline" onClick={handleClose} className="flex-1" disabled={submitting}>
            Отмена
          </Button>
          <Button
            onClick={handleSubmit}
            disabled={selected == null || submitting || loading}
            className="flex-1"
          >
            {submitting ? 'Переход в VK...' : 'Подключить'}
          </Button>
        </div>
      </DialogContent>
    </Dialog>
  );
}
