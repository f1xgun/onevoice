'use client';

import { useState } from 'react';
import { toast } from 'sonner';
import { api } from '@/lib/api';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Dialog, DialogContent, DialogHeader, DialogTitle } from '@/components/ui/dialog';

interface Props {
  open: boolean;
  onClose: () => void;
}

export function VKCommunityModal({ open, onClose }: Props) {
  const [step, setStep] = useState(1);
  const [groupId, setGroupId] = useState('');
  const [token, setToken] = useState('');
  const [loading, setLoading] = useState(false);

  function extractGroupId(input: string): string {
    const trimmed = input.trim();
    if (/^\d+$/.test(trimmed)) return trimmed;
    const clubMatch = trimmed.match(/(?:club|public)(\d+)/);
    if (clubMatch) return clubMatch[1];
    return trimmed;
  }

  async function handleConnect() {
    const id = extractGroupId(groupId);
    if (!id || !token.trim()) {
      toast.error('Заполните оба поля');
      return;
    }
    setLoading(true);
    try {
      await api.post('/integrations/vk/connect', {
        group_id: id,
        access_token: token.trim(),
      });
      toast.success('VK сообщество подключено!');
      handleClose();
    } catch (err: unknown) {
      const msg =
        (err as { response?: { data?: { error?: string } } })?.response?.data?.error ||
        'Ошибка подключения';
      toast.error(msg);
      setLoading(false);
    }
  }

  function handleClose() {
    setStep(1);
    setGroupId('');
    setToken('');
    setLoading(false);
    onClose();
  }

  return (
    <Dialog open={open} onOpenChange={(v) => !v && handleClose()}>
      <DialogContent className="max-w-lg">
        <DialogHeader>
          <DialogTitle>Подключить сообщество VK</DialogTitle>
        </DialogHeader>

        {step === 1 && (
          <div className="space-y-4">
            <div>
              <label className="mb-1 block text-sm font-medium">ID сообщества</label>
              <Input
                placeholder="vk.com/club123456789 или 123456789"
                value={groupId}
                onChange={(e) => setGroupId(e.target.value)}
              />
              <p className="mt-1 text-xs text-gray-400">
                Откройте сообщество → в адресной строке число после &quot;club&quot;
              </p>
            </div>

            <Button onClick={() => setStep(2)} disabled={!groupId.trim()} className="w-full">
              Далее
            </Button>
          </div>
        )}

        {step === 2 && (
          <div className="space-y-4">
            <div className="rounded-lg bg-blue-50 p-3 text-sm text-blue-800">
              <p className="mb-2 font-medium">Создайте ключ доступа API:</p>
              <ol className="list-inside list-decimal space-y-1 text-xs">
                <li>
                  Откройте{' '}
                  <a
                    href={`https://vk.com/club${extractGroupId(groupId)}?act=tokens`}
                    target="_blank"
                    rel="noopener noreferrer"
                    className="font-medium underline"
                  >
                    настройки API сообщества
                  </a>
                </li>
                <li>Нажмите &quot;Создать ключ&quot;</li>
                <li>
                  Отметьте права: <strong>Управление сообществом</strong>,{' '}
                  <strong>Доступ к стене</strong>, <strong>Фотографии</strong>
                </li>
                <li>Скопируйте полученный ключ и вставьте ниже</li>
              </ol>
            </div>

            <div>
              <label className="mb-1 block text-sm font-medium">Ключ доступа API</label>
              <Input
                placeholder="vk1.a.xxxx..."
                value={token}
                onChange={(e) => setToken(e.target.value)}
                type="password"
              />
            </div>

            <div className="flex gap-2">
              <Button variant="outline" onClick={() => setStep(1)} className="flex-1">
                Назад
              </Button>
              <Button
                onClick={handleConnect}
                disabled={loading || !token.trim()}
                className="flex-1"
              >
                {loading ? 'Подключение...' : 'Подключить'}
              </Button>
            </div>
          </div>
        )}
      </DialogContent>
    </Dialog>
  );
}
