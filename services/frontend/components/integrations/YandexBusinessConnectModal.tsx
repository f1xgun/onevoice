'use client';

import { useEffect, useRef, useState } from 'react';
import { useTranslations } from 'next-intl';
import { toast } from 'sonner';
import { useQueryClient } from '@tanstack/react-query';
import { Loader2 } from 'lucide-react';
import { api } from '@/lib/api';
import { API_PATHS } from '@/lib/constants/apiPaths';
import { QUERY_KEYS } from '@/lib/constants/queryKeys';
import { RU_PLURAL_PAUCAL_UPPER, RU_PLURAL_TEEN_LOWER, RU_PLURAL_TEEN_UPPER } from '@/lib/plural';
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
  const tYa = useTranslations('integrations.yandexBusiness');
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
      qc.invalidateQueries({ queryKey: QUERY_KEYS.INTEGRATIONS });
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
          <DialogTitle>{tYa('title')}</DialogTitle>
        </DialogHeader>

        {step === 'paste' && (
          <form onSubmit={handleSearchCompanies} className="space-y-4">
            <div className="space-y-2 rounded-lg bg-amber-50 p-3 text-sm text-amber-900">
              <p className="font-medium">{tYa('whyCookies')}</p>
              <p>{tYa('whyCookiesBody')}</p>
            </div>

            <details className="rounded-lg border p-3 text-sm">
              <summary className="cursor-pointer font-medium">{tYa('howToCopy')}</summary>
              <ol className="mt-3 space-y-2 pl-5 text-gray-700 [&>li]:list-decimal">
                <li>
                  {tYa('step1Prefix')}
                  <a
                    href="https://chromewebstore.google.com/detail/cookie-editor/hlkenndednhfkekhgcdicdfddnkalmdm"
                    target="_blank"
                    rel="noreferrer"
                    className="text-blue-600 underline"
                  >
                    {tYa('step1Link')}
                  </a>{' '}
                  {tYa('step1Browsers')}
                </li>
                <li>
                  {tYa('step2Prefix')}
                  <span className="font-mono text-xs">{tYa('step2Domain')}</span>
                  {tYa('step2Suffix')}
                </li>
                <li>
                  {tYa('step3Prefix')}
                  <b>{tYa('step3Export')}</b>
                  {tYa('step3Arrow')}
                  <b>{tYa('step3ExportJson')}</b>
                  {tYa('step3Suffix')}
                </li>
                <li>{tYa('step4')}</li>
              </ol>
              <p className="mt-3 text-xs text-gray-500">
                {tYa('alternativePrefix')}
                <span className="font-mono">{tYa('alternativeSession')}</span>
                {tYa('alternativeSuffix')}
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
                {tYa('cancel')}
              </Button>
              <Button type="submit" disabled={!canSearch} className="flex-1">
                {tYa('next')}
              </Button>
            </div>
          </form>
        )}

        {step === 'searching' && (
          <div className="flex flex-col items-center gap-4 py-12">
            <Loader2 className="h-10 w-10 animate-spin text-gray-500" />
            <div className="text-center">
              <p className="font-medium text-ink">{tYa('searching')}</p>
              <p className="mt-1 text-sm text-gray-500">{tYa('searchingBody')}</p>
            </div>
          </div>
        )}

        {step === 'pick' && (
          <form onSubmit={handlePickSubmit} className="space-y-4">
            <p className="text-sm text-gray-700">
              {tYa('found', { count: companies.length, ending: plural(companies.length) })}
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
                {tYa('back')}
              </Button>
              <Button type="submit" disabled={!selectedPermalink} className="flex-1">
                {tYa('connect')}
              </Button>
            </div>
          </form>
        )}

        {step === 'connecting' && (
          <div className="flex flex-col items-center gap-4 py-12">
            <Loader2 className="h-10 w-10 animate-spin text-gray-500" />
            <p className="text-sm text-gray-600">{tYa('connecting')}</p>
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
  // The 11–14 range (RU_PLURAL_TEEN_LOWER..RU_PLURAL_TEEN_UPPER) always
  // takes the genitive-plural ending, overriding the last-digit rule.
  const isTeen = mod100 >= RU_PLURAL_TEEN_LOWER && mod100 <= RU_PLURAL_TEEN_UPPER;
  if (mod10 === 1 && !isTeen) return 'ация';
  if (mod10 >= 2 && mod10 <= RU_PLURAL_PAUCAL_UPPER && !isTeen) return 'ации';
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
  const tYa = useTranslations('integrations.yandexBusiness');
  if (!hasInput) return null;
  if (probing) {
    return <p className="text-sm text-gray-500">{tYa('checking')}</p>;
  }
  if (!probe) return null;

  if (!probe.ok) {
    return (
      <p className="text-sm text-red-600">
        {tYa('probeError', { error: probe.error || tYa('formatNotRecognized') })}
      </p>
    );
  }

  const formatLabel = probe.format ? FORMAT_LABELS[probe.format] : 'формат';

  return (
    <div className="space-y-1 text-sm">
      <p className="text-green-700">
        {tYa('formatRecognized', { format: formatLabel })}
        {probe.session_valid === true && probe.username && (
          <>
            {tYa('loggedInAs')}
            <span className="font-medium">{probe.username}</span>
          </>
        )}
        {probe.session_valid === true && !probe.username && <>{tYa('sessionActive')}</>}
      </p>
      {probe.session_valid === false && <p className="text-amber-700">{tYa('sessionExpired')}</p>}
      {probe.session_valid === undefined && (
        <p className="text-gray-500">{tYa('sessionUnchecked')}</p>
      )}
      {probe.warnings?.map((w, i) => (
        <p key={i} className="text-amber-700">
          ⚠ {w}
        </p>
      ))}
    </div>
  );
}
