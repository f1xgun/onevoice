'use client';

import { useEffect, useRef, useState } from 'react';
import { useTranslations } from 'next-intl';
import { toast } from 'sonner';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { Check, Copy, Loader2 } from 'lucide-react';
import { bizApi } from '@/lib/api/business-api';
import { INTEGRATION_ENDPOINTS } from '@/lib/constants/bizApiPaths';
import { QUERY_KEYS } from '@/lib/constants/queryKeys';
import { useBusinessStore } from '@/lib/stores/business';
import { Button } from '@/components/ui/button';
import { Dialog, DialogContent, DialogHeader, DialogTitle } from '@/components/ui/dialog';
import { Input } from '@/components/ui/input';
import { Textarea } from '@/components/ui/textarea';
import { STALE_TIME_5_MIN } from '@/lib/constants/cacheTTL';
import { extractApiErrorCode, useMapEmailVerificationError } from '@/lib/resolveErrorMap';

interface Props {
  open: boolean;
  onClose: () => void;
}

interface DelegatedConfig {
  available: boolean;
  rep_login: string;
}

const COPIED_RESET_MS = 2000;

// YandexBusinessConnectModal picks the connect method for Yandex.Business.
//
// Yandex exposes no OAuth API for the actions our automation drives (reviews,
// info, posts), so there are two ways to grant access:
//
//   1. Delegated representative (preferred, when provisioned): the owner adds
//      OneVoice's shared representative login to their organization's Access
//      section and pastes their organization's Maps link. No credential leaves
//      the owner's hands — access is a named, revocable role. Led with when
//      GET .../delegated-config reports it available.
//   2. Cookie paste (fallback): the owner exports session cookies from their
//      browser. Kept as the fallback for deployments where the delegated plane
//      isn't provisioned, and as an escape hatch for advanced users.
export function YandexBusinessConnectModal({ open, onClose }: Props) {
  const tYa = useTranslations('integrations.yandexBusiness');
  const activeBusinessId = useBusinessStore((s) => s.activeBusinessId);
  const [forcePaste, setForcePaste] = useState(false);

  const { data: config, isLoading: configLoading } = useQuery<DelegatedConfig>({
    queryKey: QUERY_KEYS.BUSINESS_YANDEX_DELEGATED_CONFIG(activeBusinessId),
    queryFn: async () => {
      const path = INTEGRATION_ENDPOINTS.yandex_business?.delegatedConfig;
      const { data } = await bizApi(activeBusinessId!).get<DelegatedConfig>(path!);
      return data;
    },
    enabled: open && !!activeBusinessId,
    staleTime: STALE_TIME_5_MIN,
  });

  function handleClose() {
    setForcePaste(false);
    onClose();
  }

  const delegatedAvailable = !!config?.available && !!config.rep_login;
  const showPaste = !delegatedAvailable || forcePaste;

  return (
    <Dialog open={open} onOpenChange={(v) => !v && handleClose()}>
      <DialogContent className="max-w-2xl">
        <DialogHeader>
          <DialogTitle>{tYa('title')}</DialogTitle>
        </DialogHeader>

        {configLoading ? (
          <div className="flex justify-center py-12">
            <Loader2 className="h-8 w-8 animate-spin text-gray-400" />
          </div>
        ) : showPaste ? (
          <PasteFlow
            activeBusinessId={activeBusinessId}
            onClose={handleClose}
            onBack={delegatedAvailable ? () => setForcePaste(false) : undefined}
          />
        ) : (
          <DelegatedFlow
            activeBusinessId={activeBusinessId}
            repLogin={config!.rep_login}
            onClose={handleClose}
            onUsePaste={() => setForcePaste(true)}
          />
        )}
      </DialogContent>
    </Dialog>
  );
}

type DelegatedStep = 'intro' | 'working' | 'unverified';

function DelegatedFlow({
  activeBusinessId,
  repLogin,
  onClose,
  onUsePaste,
}: {
  activeBusinessId: string | null;
  repLogin: string;
  onClose: () => void;
  onUsePaste: () => void;
}) {
  const tD = useTranslations('integrations.yandexBusiness.delegated');
  const qc = useQueryClient();
  const mapVerifyError = useMapEmailVerificationError();
  const [step, setStep] = useState<DelegatedStep>('intro');
  const [link, setLink] = useState('');
  const [permalink, setPermalink] = useState('');
  const [copied, setCopied] = useState(false);

  async function copyRepLogin() {
    try {
      await navigator.clipboard.writeText(repLogin);
      setCopied(true);
      toast.success(tD('copiedToast'));
      setTimeout(() => setCopied(false), COPIED_RESET_MS);
    } catch {
      toast.error(tD('copyFailed'));
    }
  }

  async function runVerify(pl: string): Promise<boolean> {
    const verifyPath = INTEGRATION_ENDPOINTS.yandex_business?.verifyAccess;
    if (!verifyPath || !activeBusinessId) return false;
    try {
      const { data } = await bizApi(activeBusinessId).post<{ access_verified: boolean }>(
        verifyPath,
        {
          permalink: pl,
        }
      );
      return !!data.access_verified;
    } catch {
      return false;
    }
  }

  function finishVerified() {
    toast.success(tD('verifiedToast'));
    if (activeBusinessId) {
      qc.invalidateQueries({ queryKey: QUERY_KEYS.BUSINESS_INTEGRATIONS(activeBusinessId) });
    }
    onClose();
  }

  async function handleConnect(e: React.FormEvent) {
    e.preventDefault();
    const trimmed = link.trim();
    if (!trimmed || !activeBusinessId) return;
    const connectPath = INTEGRATION_ENDPOINTS.yandex_business?.connectDelegated;
    if (!connectPath) return;
    setStep('working');
    try {
      const { data: integ } = await bizApi(activeBusinessId).post<{ externalId: string }>(
        connectPath,
        {
          maps_url: trimmed,
        }
      );
      const pl = integ.externalId;
      setPermalink(pl);
      qc.invalidateQueries({ queryKey: QUERY_KEYS.BUSINESS_INTEGRATIONS(activeBusinessId) });
      if (await runVerify(pl)) {
        finishVerified();
        return;
      }
      setStep('unverified');
    } catch (err: unknown) {
      const msg = mapVerifyError(err) ?? extractApiErrorCode(err) ?? tD('connectFailed');
      toast.error(msg);
      setStep('intro');
    }
  }

  async function handleRetry() {
    if (!permalink) {
      setStep('intro');
      return;
    }
    setStep('working');
    if (await runVerify(permalink)) {
      finishVerified();
      return;
    }
    setStep('unverified');
  }

  if (step === 'working') {
    return (
      <div className="flex flex-col items-center gap-4 py-12">
        <Loader2 className="h-10 w-10 animate-spin text-gray-500" />
        <div className="text-center">
          <p className="font-medium text-ink">{tD('working')}</p>
          <p className="mt-1 text-sm text-gray-500">{tD('workingBody')}</p>
        </div>
      </div>
    );
  }

  if (step === 'unverified') {
    return (
      <div className="space-y-4">
        <div className="space-y-2 rounded-lg bg-amber-50 p-4 text-sm text-amber-900">
          <p className="font-medium">{tD('unverifiedTitle')}</p>
          <p>{tD('unverifiedBody')}</p>
        </div>
        <div className="flex gap-2 pt-2">
          <Button type="button" variant="outline" onClick={onClose} className="flex-1">
            {tD('done')}
          </Button>
          <Button type="button" onClick={handleRetry} className="flex-1">
            {tD('retry')}
          </Button>
        </div>
      </div>
    );
  }

  return (
    <form onSubmit={handleConnect} className="space-y-4">
      <p className="text-sm text-ink-mid">{tD('intro')}</p>

      <ol className="space-y-3 text-sm text-ink-mid">
        <li className="flex gap-2">
          <span className="font-medium text-ink">1.</span>
          <span>{tD('step1')}</span>
        </li>
        <li className="flex gap-2">
          <span className="font-medium text-ink">2.</span>
          <span>{tD('step2')}</span>
        </li>
      </ol>

      <div className="space-y-1.5 rounded-lg border border-line-soft bg-paper-sunken p-3">
        <span className="text-xs text-ink-soft">{tD('repLoginLabel')}</span>
        <div className="flex items-center gap-2">
          <code className="min-w-0 flex-1 truncate rounded bg-paper px-2 py-1.5 font-mono text-sm text-ink">
            {repLogin}
          </code>
          <Button type="button" variant="outline" size="sm" onClick={copyRepLogin}>
            {copied ? (
              <Check className="mr-1 h-3.5 w-3.5 text-green-600" />
            ) : (
              <Copy className="mr-1 h-3.5 w-3.5" />
            )}
            {copied ? tD('copied') : tD('copy')}
          </Button>
        </div>
      </div>

      <div className="space-y-1.5">
        <label className="text-sm text-ink-mid" htmlFor="yandex-delegated-link">
          {tD('linkLabel')}
        </label>
        <Input
          id="yandex-delegated-link"
          autoFocus
          spellCheck={false}
          value={link}
          onChange={(e) => setLink(e.target.value)}
          placeholder={tD('linkPlaceholder')}
          className="font-mono text-xs"
        />
        <p className="text-xs text-ink-soft">{tD('linkHint')}</p>
      </div>

      <div className="flex gap-2 pt-2">
        <Button type="button" variant="outline" onClick={onClose} className="flex-1">
          {tD('cancel')}
        </Button>
        <Button type="submit" disabled={!link.trim()} className="flex-1">
          {tD('connect')}
        </Button>
      </div>

      <button
        type="button"
        onClick={onUsePaste}
        className="w-full pt-1 text-center text-xs text-ink-soft underline hover:text-ink-mid"
      >
        {tD('pasteFallback')}
      </button>
    </form>
  );
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

const PROBE_DEBOUNCE_MS = 500;

type PasteStep = 'paste' | 'searching' | 'pick' | 'connecting';

// PasteFlow is the cookie-based connect path: the owner pastes session cookies,
// a live probe validates their format/session, list_companies enumerates the
// orgs reachable with those cookies, and the chosen permalink is stored. onBack
// (present only when the delegated flow is available) offers a return to it.
function PasteFlow({
  activeBusinessId,
  onClose,
  onBack,
}: {
  activeBusinessId: string | null;
  onClose: () => void;
  onBack?: () => void;
}) {
  const tYa = useTranslations('integrations.yandexBusiness');
  const qc = useQueryClient();
  const mapVerifyError = useMapEmailVerificationError();
  const [step, setStep] = useState<PasteStep>('paste');
  const [value, setValue] = useState('');
  const [probe, setProbe] = useState<ProbeResponse | null>(null);
  const [probing, setProbing] = useState(false);
  const [companies, setCompanies] = useState<CompanyEntry[]>([]);
  const [selectedPermalink, setSelectedPermalink] = useState<string>('');
  const probeAbortRef = useRef<AbortController | null>(null);

  useEffect(() => {
    if (step !== 'paste') return;
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
        if (!activeBusinessId) return;
        const probePath = INTEGRATION_ENDPOINTS.yandex_business?.probe;
        if (!probePath) return;
        const { data } = await bizApi(activeBusinessId).post<ProbeResponse>(
          probePath,
          { cookies: trimmed },
          { signal: controller.signal }
        );
        setProbe(data);
      } catch (err: unknown) {
        if ((err as { name?: string })?.name === 'CanceledError') return;
        setProbe({ ok: false, error: tYa('probeFailed') });
      } finally {
        if (!controller.signal.aborted) setProbing(false);
      }
    }, PROBE_DEBOUNCE_MS);

    return () => clearTimeout(handle);
  }, [value, step, activeBusinessId, tYa]);

  async function handleSearchCompanies(e: React.FormEvent) {
    e.preventDefault();
    if (!probe?.ok) return;
    setStep('searching');
    try {
      if (!activeBusinessId) return;
      const companiesPath = INTEGRATION_ENDPOINTS.yandex_business?.companies;
      if (!companiesPath) return;
      const { data } = await bizApi(activeBusinessId).post<{ companies: CompanyEntry[] }>(
        companiesPath,
        {
          cookies: value.trim(),
        }
      );
      const list = data.companies ?? [];
      if (list.length === 0) {
        toast.error(tYa('noOrgs'));
        setStep('paste');
        return;
      }
      setCompanies(list);
      setSelectedPermalink(list[0].permalink);
      if (list.length === 1) {
        await connectWith(list[0]);
        return;
      }
      setStep('pick');
    } catch (err: unknown) {
      const msg = mapVerifyError(err) ?? extractApiErrorCode(err) ?? tYa('fetchOrgsFailed');
      toast.error(msg);
      setStep('paste');
    }
  }

  async function connectWith(company: CompanyEntry) {
    setStep('connecting');
    try {
      if (!activeBusinessId) return;
      const connectPath = INTEGRATION_ENDPOINTS.yandex_business?.connect;
      if (!connectPath) return;
      await bizApi(activeBusinessId).post(connectPath, {
        cookies: value.trim(),
        permalink: company.permalink,
        business_name: company.name,
      });
      toast.success(tYa('connectedToast', { name: company.name || company.permalink }));
      qc.invalidateQueries({ queryKey: QUERY_KEYS.BUSINESS_INTEGRATIONS(activeBusinessId) });
      onClose();
    } catch (err: unknown) {
      const msg = mapVerifyError(err) ?? extractApiErrorCode(err) ?? tYa('connectFailed');
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

  if (step === 'searching') {
    return (
      <div className="flex flex-col items-center gap-4 py-12">
        <Loader2 className="h-10 w-10 animate-spin text-gray-500" />
        <div className="text-center">
          <p className="font-medium text-ink">{tYa('searching')}</p>
          <p className="mt-1 text-sm text-gray-500">{tYa('searchingBody')}</p>
        </div>
      </div>
    );
  }

  if (step === 'connecting') {
    return (
      <div className="flex flex-col items-center gap-4 py-12">
        <Loader2 className="h-10 w-10 animate-spin text-gray-500" />
        <p className="text-sm text-gray-600">{tYa('connecting')}</p>
      </div>
    );
  }

  if (step === 'pick') {
    return (
      <form onSubmit={handlePickSubmit} className="space-y-4">
        <p className="text-sm text-gray-700">{tYa('found', { count: companies.length })}</p>
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
                  {c.name || tYa('unnamedOrg')}
                </div>
                <div className="truncate font-mono text-[11px] text-gray-500">{c.permalink}</div>
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
    );
  }

  return (
    <form onSubmit={handleSearchCompanies} className="space-y-4">
      {onBack && (
        <button
          type="button"
          onClick={onBack}
          className="text-xs text-ink-soft underline hover:text-ink-mid"
        >
          {tYa('delegated.backToDelegated')}
        </button>
      )}

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
          placeholder={tYa('pastePlaceholder')}
          rows={6}
          className="font-mono text-xs"
        />
        <ProbeStatus probing={probing} probe={probe} hasInput={value.trim().length > 0} />
      </div>

      <div className="flex gap-2 pt-2">
        <Button type="button" variant="outline" onClick={onClose} className="flex-1">
          {tYa('cancel')}
        </Button>
        <Button type="submit" disabled={!canSearch} className="flex-1">
          {tYa('next')}
        </Button>
      </div>
    </form>
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

  const formatLabel = probe.format
    ? tYa(`formatLabels.${probe.format}`)
    : tYa('formatLabels.generic');

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
