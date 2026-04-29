'use client';

import { useEffect, useRef, useState } from 'react';
import { toast } from 'sonner';
import { useQueryClient } from '@tanstack/react-query';
import { api } from '@/lib/api';
import { Button } from '@/components/ui/button';
import { Dialog, DialogContent, DialogHeader, DialogTitle } from '@/components/ui/dialog';
import { Textarea } from '@/components/ui/textarea';

interface Props {
  open: boolean;
  onClose: () => void;
}

interface ProbeResponse {
  ok: boolean;
  format?: 'json' | 'cookie_header' | 'session_id_value';
  session_valid?: boolean;
  username?: string;
  warnings?: string[];
  error?: string;
}

const FORMAT_LABELS: Record<NonNullable<ProbeResponse['format']>, string> = {
  json: 'JSON-массив (Cookie-Editor)',
  cookie_header: 'Cookie-заголовок',
  session_id_value: 'значение Session_id',
};

const PROBE_DEBOUNCE_MS = 500;

// YandexBusinessConnectModal collects Yandex.Business session cookies
// pasted from the user's browser. Yandex doesn't expose an OAuth API for
// the actions our RPA agent automates (reviews, info, posts), so the
// agent needs real session cookies to drive Playwright. The modal accepts
// three formats interchangeably and live-validates the paste against
// passport.yandex.ru via the /probe endpoint.
export function YandexBusinessConnectModal({ open, onClose }: Props) {
  const qc = useQueryClient();
  const [value, setValue] = useState('');
  const [probe, setProbe] = useState<ProbeResponse | null>(null);
  const [probing, setProbing] = useState(false);
  const [connecting, setConnecting] = useState(false);
  const probeAbortRef = useRef<AbortController | null>(null);

  // Debounced probe as the user pastes / types.
  useEffect(() => {
    if (!open) return;
    const trimmed = value.trim();
    if (!trimmed) {
      setProbe(null);
      setProbing(false);
      probeAbortRef.current?.abort();
      return;
    }

    const handle = setTimeout(async () => {
      probeAbortRef.current?.abort();
      const controller = new AbortController();
      probeAbortRef.current = controller;
      setProbing(true);
      try {
        const { data } = await api.post<ProbeResponse>(
          '/integrations/yandex_business/probe',
          { cookies: trimmed },
          { signal: controller.signal }
        );
        setProbe(data);
      } catch (err: unknown) {
        if ((err as { name?: string })?.name === 'CanceledError') return;
        setProbe({
          ok: false,
          error: 'Не удалось проверить — попробуйте ещё раз',
        });
      } finally {
        if (!controller.signal.aborted) setProbing(false);
      }
    }, PROBE_DEBOUNCE_MS);

    return () => clearTimeout(handle);
  }, [value, open]);

  function handleClose() {
    probeAbortRef.current?.abort();
    setValue('');
    setProbe(null);
    setProbing(false);
    setConnecting(false);
    onClose();
  }

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault();
    if (!probe?.ok || connecting) return;
    setConnecting(true);
    try {
      await api.post('/integrations/yandex_business/connect', { cookies: value.trim() });
      toast.success('Яндекс.Бизнес подключён');
      qc.invalidateQueries({ queryKey: ['integrations'] });
      handleClose();
    } catch (err: unknown) {
      const msg =
        (err as { response?: { data?: { error?: string } } })?.response?.data?.error ||
        'Не удалось подключить';
      toast.error(msg);
      setConnecting(false);
    }
  }

  // Submit is allowed once the format parses cleanly. We don't block on
  // SessionValid being true — the live probe can fail on Yandex anti-bot
  // even with valid cookies, and the agent's Playwright canary will catch
  // a truly dead session on the first real call.
  const canSubmit = !!probe?.ok && !connecting;

  return (
    <Dialog open={open} onOpenChange={(v) => !v && handleClose()}>
      <DialogContent className="max-w-2xl">
        <DialogHeader>
          <DialogTitle>Подключить Яндекс.Бизнес</DialogTitle>
        </DialogHeader>

        <form onSubmit={handleSubmit} className="space-y-4">
          <div className="space-y-2 rounded-lg bg-amber-50 p-3 text-sm text-amber-900">
            <p className="font-medium">Зачем нужны cookies?</p>
            <p>
              У Яндекс.Бизнеса нет публичного API для управления отзывами и информацией о компании,
              поэтому OneVoice работает через ваш сеанс — как если бы вы открыли сайт сами. Cookies
              хранятся зашифрованно и используются только для действий, которые вы инициируете.
            </p>
          </div>

          <details className="rounded-lg border p-3 text-sm">
            <summary className="cursor-pointer font-medium">Как скопировать cookies</summary>
            <ol className="mt-3 space-y-2 pl-5 text-gray-700 [&>li]:list-decimal">
              <li>
                Установите расширение{' '}
                <a
                  href="https://chromewebstore.google.com/detail/cookie-editor/hlkenndednhfkekhgcdicdfddnkalmdm"
                  target="_blank"
                  rel="noreferrer"
                  className="text-blue-600 underline"
                >
                  Cookie-Editor
                </a>{' '}
                (Chrome / Edge / Firefox).
              </li>
              <li>
                Откройте <span className="font-mono text-xs">business.yandex.ru</span> и войдите в
                нужный аккаунт.
              </li>
              <li>
                Нажмите на иконку расширения → <b>Export</b> → <b>Export as JSON</b> — JSON
                автоматически скопируется в буфер.
              </li>
              <li>Вставьте в поле ниже.</li>
            </ol>
            <p className="mt-3 text-xs text-gray-500">
              Альтернативно: можно вставить только значение{' '}
              <span className="font-mono">Session_id</span> или «сырой» Cookie-заголовок из DevTools
              → Network → Request Headers.
            </p>
          </details>

          <div className="space-y-2">
            <Textarea
              autoFocus
              value={value}
              onChange={(e) => setValue(e.target.value)}
              placeholder='Вставьте сюда: JSON-массив, "Cookie:" заголовок, или Session_id=...'
              rows={6}
              className="font-mono text-xs"
              disabled={connecting}
            />
            <ProbeStatus probing={probing} probe={probe} hasInput={value.trim().length > 0} />
          </div>

          <div className="flex gap-2 pt-2">
            <Button
              type="button"
              variant="outline"
              onClick={handleClose}
              className="flex-1"
              disabled={connecting}
            >
              Отмена
            </Button>
            <Button type="submit" disabled={!canSubmit} className="flex-1">
              {connecting ? 'Подключение…' : 'Подключить'}
            </Button>
          </div>
        </form>
      </DialogContent>
    </Dialog>
  );
}

function ProbeStatus({
  probing,
  probe,
  hasInput,
}: {
  probing: boolean;
  probe: ProbeResponse | null;
  hasInput: boolean;
}) {
  if (!hasInput) return null;
  if (probing) {
    return <p className="text-sm text-gray-500">Проверяем…</p>;
  }
  if (!probe) return null;

  if (!probe.ok) {
    return <p className="text-sm text-red-600">✗ {probe.error || 'Не распознан формат'}</p>;
  }

  const formatLabel = probe.format ? FORMAT_LABELS[probe.format] : 'формат';

  return (
    <div className="space-y-1 text-sm">
      <p className="text-green-700">
        ✓ Формат распознан ({formatLabel})
        {probe.session_valid === true && probe.username && (
          <>
            {' '}
            — вошли как <span className="font-medium">{probe.username}</span>
          </>
        )}
        {probe.session_valid === true && !probe.username && <> — сеанс активен</>}
      </p>
      {probe.session_valid === false && (
        <p className="text-amber-700">
          ⚠ Сеанс выглядит истёкшим — возможно, нужно заново войти в Яндекс и скопировать cookies
          снова. Можно подключить и проверить — ошибка вылезет при первом действии.
        </p>
      )}
      {probe.session_valid === undefined && (
        <p className="text-gray-500">
          Не удалось проверить сеанс с нашей стороны (антибот Яндекса). Сеанс будет проверен при
          первом обращении.
        </p>
      )}
      {probe.warnings?.map((w, i) => (
        <p key={i} className="text-amber-700">
          ⚠ {w}
        </p>
      ))}
    </div>
  );
}
