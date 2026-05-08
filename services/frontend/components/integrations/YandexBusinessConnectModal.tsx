'use client';

import { useEffect, useRef, useState } from 'react';
import { toast } from 'sonner';
import { useQueryClient } from '@tanstack/react-query';
import { Loader2 } from 'lucide-react';
import { api } from '@/lib/api';
import { API_PATHS } from '@/lib/constants/apiPaths';
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

interface CompanyEntry {
  permalink: string;
  name: string;
}

const FORMAT_LABELS: Record<NonNullable<ProbeResponse['format']>, string> = {
  json: 'JSON-массив (Cookie-Editor)',
  cookie_header: 'Cookie-заголовок',
  session_id_value: 'значение Session_id',
};

const PROBE_DEBOUNCE_MS = 500;

type Step = 'paste' | 'searching' | 'pick' | 'connecting';

// YandexBusinessConnectModal collects Yandex.Business session cookies
// pasted from the user's browser. Yandex doesn't expose an OAuth API for
// the actions our RPA agent automates (reviews, info, posts), so the
// agent needs real session cookies to drive Playwright.
//
// Flow:
//   paste     → user pastes cookies, live probe validates format/session
//   searching → /companies dispatches list_companies RPA (~25–45s)
//   pick      → if >1 org, radio picker; if 1, auto-skip
//   connecting → final /connect with chosen permalink + name
export function YandexBusinessConnectModal({ open, onClose }: Props) {
  const qc = useQueryClient();
  const [step, setStep] = useState<Step>('paste');
  const [value, setValue] = useState('');
  const [probe, setProbe] = useState<ProbeResponse | null>(null);
  const [probing, setProbing] = useState(false);
  const [companies, setCompanies] = useState<CompanyEntry[]>([]);
  const [selectedPermalink, setSelectedPermalink] = useState<string>('');
  const probeAbortRef = useRef<AbortController | null>(null);

  // Debounced probe as the user pastes / types (only on the paste step).
  useEffect(() => {
    if (!open || step !== 'paste') return;
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
        setProbe({ ok: false, error: 'Не удалось проверить — попробуйте ещё раз' });
      } finally {
        if (!controller.signal.aborted) setProbing(false);
      }
    }, PROBE_DEBOUNCE_MS);

    return () => clearTimeout(handle);
  }, [value, open, step]);

  function resetState() {
    probeAbortRef.current?.abort();
    setStep('paste');
    setValue('');
    setProbe(null);
    setProbing(false);
    setCompanies([]);
    setSelectedPermalink('');
  }

  function handleClose() {
    resetState();
    onClose();
  }

  // Step 1 → 2: fetch companies via the agent's Playwright RPA.
  async function handleSearchCompanies(e: React.FormEvent) {
    e.preventDefault();
    if (!probe?.ok) return;
    setStep('searching');
    try {
      const { data } = await api.post<{ companies: CompanyEntry[] }>(
        '/integrations/yandex_business/companies',
        { cookies: value.trim() }
      );
      const list = data.companies ?? [];
      if (list.length === 0) {
        toast.error('В аккаунте Яндекса нет ни одной организации.');
        setStep('paste');
        return;
      }
      setCompanies(list);
      setSelectedPermalink(list[0].permalink);
      if (list.length === 1) {
        // Single org — skip the picker entirely.
        await connectWith(list[0]);
        return;
      }
      setStep('pick');
    } catch (err: unknown) {
      const msg =
        (err as { response?: { data?: { error?: string } } })?.response?.data?.error ||
        'Не удалось получить список организаций. Попробуйте ещё раз.';
      toast.error(msg);
      setStep('paste');
    }
  }

  // Step 2 → 3: send the final connect with the chosen permalink + name.
  async function connectWith(company: CompanyEntry) {
    setStep('connecting');
    try {
      await api.post(API_PATHS.INTEGRATIONS.YANDEX_BUSINESS_CONNECT, {
        cookies: value.trim(),
        permalink: company.permalink,
        business_name: company.name,
      });
      toast.success(`Подключено: ${company.name || company.permalink}`);
      qc.invalidateQueries({ queryKey: ['integrations'] });
      handleClose();
    } catch (err: unknown) {
      const msg =
        (err as { response?: { data?: { error?: string } } })?.response?.data?.error ||
        'Не удалось подключить';
      toast.error(msg);
      setStep('pick');
    }
  }

  async function handlePickSubmit(e: React.FormEvent) {
    e.preventDefault();
    const chosen = companies.find((c) => c.permalink === selectedPermalink);
    if (chosen) await connectWith(chosen);
  }

  const canSearch = !!probe?.ok && step === 'paste';

  return (
    <Dialog open={open} onOpenChange={(v) => !v && handleClose()}>
      <DialogContent className="max-w-2xl">
        <DialogHeader>
          <DialogTitle>Подключить Яндекс.Бизнес</DialogTitle>
        </DialogHeader>

        {step === 'paste' && (
          <form onSubmit={handleSearchCompanies} className="space-y-4">
            <div className="space-y-2 rounded-lg bg-amber-50 p-3 text-sm text-amber-900">
              <p className="font-medium">Зачем нужны cookies?</p>
              <p>
                У Яндекс.Бизнеса нет публичного API для управления отзывами и информацией о
                компании, поэтому OneVoice работает через ваш сеанс — как если бы вы открыли сайт
                сами. Cookies хранятся зашифрованно и используются только для действий, которые вы
                инициируете.
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
                <span className="font-mono">Session_id</span> или «сырой» Cookie-заголовок из
                DevTools → Network → Request Headers.
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
              />
              <ProbeStatus probing={probing} probe={probe} hasInput={value.trim().length > 0} />
            </div>

            <div className="flex gap-2 pt-2">
              <Button type="button" variant="outline" onClick={handleClose} className="flex-1">
                Отмена
              </Button>
              <Button type="submit" disabled={!canSearch} className="flex-1">
                Далее
              </Button>
            </div>
          </form>
        )}

        {step === 'searching' && (
          <div className="flex flex-col items-center gap-4 py-12">
            <Loader2 className="h-10 w-10 animate-spin text-gray-500" />
            <div className="text-center">
              <p className="font-medium text-ink">Ищем ваши организации в Яндексе…</p>
              <p className="mt-1 text-sm text-gray-500">
                Это может занять до минуты. Мы открываем Яндекс.Бизнес от вашего имени, чтобы
                получить список.
              </p>
            </div>
          </div>
        )}

        {step === 'pick' && (
          <form onSubmit={handlePickSubmit} className="space-y-4">
            <p className="text-sm text-gray-700">
              Найдено {companies.length} организ{plural(companies.length)}. Какую подключить к
              OneVoice?
            </p>
            <div className="max-h-80 space-y-2 overflow-y-auto">
              {companies.map((c) => (
                <label
                  key={c.permalink}
                  className={`flex cursor-pointer items-start gap-3 rounded-md border p-3 hover:bg-gray-50 ${
                    selectedPermalink === c.permalink ? 'border-blue-500 bg-blue-50' : ''
                  }`}
                >
                  <input
                    type="radio"
                    name="company"
                    value={c.permalink}
                    checked={selectedPermalink === c.permalink}
                    onChange={() => setSelectedPermalink(c.permalink)}
                    className="mt-1"
                  />
                  <div className="min-w-0 flex-1">
                    <div className="truncate text-[14px] font-medium text-ink">
                      {c.name || 'Без названия'}
                    </div>
                    <div className="truncate font-mono text-[11px] text-gray-500">
                      {c.permalink}
                    </div>
                  </div>
                </label>
              ))}
            </div>
            <div className="flex gap-2 pt-2">
              <Button
                type="button"
                variant="outline"
                onClick={() => setStep('paste')}
                className="flex-1"
              >
                Назад
              </Button>
              <Button type="submit" disabled={!selectedPermalink} className="flex-1">
                Подключить
              </Button>
            </div>
          </form>
        )}

        {step === 'connecting' && (
          <div className="flex flex-col items-center gap-4 py-12">
            <Loader2 className="h-10 w-10 animate-spin text-gray-500" />
            <p className="text-sm text-gray-600">Подключение…</p>
          </div>
        )}
      </DialogContent>
    </Dialog>
  );
}

// plural picks the right Russian noun ending: 1 → "ация", 2-4 → "ации", else → "аций".
function plural(n: number): string {
  const mod10 = n % 10;
  const mod100 = n % 100;
  if (mod10 === 1 && mod100 !== 11) return 'ация';
  if (mod10 >= 2 && mod10 <= 4 && (mod100 < 12 || mod100 > 14)) return 'ации';
  return 'аций';
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
