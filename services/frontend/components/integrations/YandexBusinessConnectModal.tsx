'use client';

import { useState } from 'react';
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
import { useForm } from 'react-hook-form';
import { zodResolver } from '@hookform/resolvers/zod';
import { z } from 'zod';
import { STALE_TIME_5_MIN } from '@/lib/constants/cacheTTL';
import {
  isEmailVerificationRequiredError,
  useMapEmailVerificationError,
} from '@/lib/resolveErrorMap';

interface Props {
  open: boolean;
  onClose: () => void;
}

interface DelegatedConfig {
  available: boolean;
  rep_login: string;
}

const COPIED_RESET_MS = 2000;

export function YandexBusinessConnectModal({ open, onClose }: Props) {
  const tYa = useTranslations('integrations.yandexBusiness');
  const activeBusinessId = useBusinessStore((s) => s.activeBusinessId);
  const mapVerifyError = useMapEmailVerificationError();

  const {
    data: config,
    isLoading: configLoading,
    error: configError,
  } = useQuery<DelegatedConfig>({
    queryKey: QUERY_KEYS.BUSINESS_YANDEX_DELEGATED_CONFIG(activeBusinessId),
    queryFn: async () => {
      const path = INTEGRATION_ENDPOINTS.yandex_business?.delegatedConfig;
      const { data } = await bizApi(activeBusinessId!).get<DelegatedConfig>(path!);
      return data;
    },
    enabled: open && !!activeBusinessId,
    staleTime: STALE_TIME_5_MIN,
    retry: false,
  });

  function handleClose() {
    onClose();
  }

  const delegatedAvailable = !!config?.available && !!config.rep_login;

  return (
    <Dialog open={open} onOpenChange={(v) => !v && handleClose()}>
      <DialogContent className="max-w-2xl">
        <DialogHeader>
          <DialogTitle>{tYa('title')}</DialogTitle>
        </DialogHeader>

        <p className="rounded-md border border-line bg-paper-sunken p-4 text-sm">
          {tYa('security')}
        </p>
        {configLoading ? (
          <div className="flex justify-center py-12">
            <Loader2 className="h-8 w-8 animate-spin text-gray-400" />
          </div>
        ) : !delegatedAvailable ? (
          <div className="space-y-4">
            <p>{tYa('preparing')}</p>
            {configError && (
              <p role="alert" className="text-sm text-destructive">
                {mapVerifyError(configError) ?? tYa('configFailed')}
              </p>
            )}
            <Button disabled>{tYa('delegated.connect')}</Button>
          </div>
        ) : (
          <DelegatedFlow
            activeBusinessId={activeBusinessId}
            repLogin={config!.rep_login}
            onClose={handleClose}
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
}: {
  activeBusinessId: string | null;
  repLogin: string;
  onClose: () => void;
}) {
  const tD = useTranslations('integrations.yandexBusiness.delegated');
  const qc = useQueryClient();
  const mapVerifyError = useMapEmailVerificationError();
  const [step, setStep] = useState<DelegatedStep>('intro');
  const [error, setError] = useState<string | null>(null);
  const [verificationBlocked, setVerificationBlocked] = useState(false);
  const { register, watch, handleSubmit } = useForm<{ link: string }>({
    resolver: zodResolver(z.object({ link: z.string().trim().min(1) })),
    defaultValues: { link: '' },
  });
  const link = watch('link');
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
    } catch (err) {
      if (isEmailVerificationRequiredError(err)) throw err;
      setError(tD('verifyFailed'));
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

  async function handleConnect() {
    const trimmed = link.trim();
    if (!trimmed || !activeBusinessId) return;
    const connectPath = INTEGRATION_ENDPOINTS.yandex_business?.connectDelegated;
    if (!connectPath) return;
    setError(null);
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
      setError(mapVerifyError(err) ?? tD('connectFailed'));
      setVerificationBlocked(isEmailVerificationRequiredError(err));
      setStep('intro');
    }
  }

  async function handleRetry() {
    if (!permalink) {
      setStep('intro');
      return;
    }
    setError(null);
    setStep('working');
    try {
      if (await runVerify(permalink)) {
        finishVerified();
        return;
      }
    } catch (err) {
      setError(mapVerifyError(err) ?? tD('verifyFailed'));
      setVerificationBlocked(isEmailVerificationRequiredError(err));
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
        {error && (
          <p role="alert" className="text-sm text-destructive">
            {error}
          </p>
        )}
        <div className="space-y-2 rounded-lg bg-amber-50 p-4 text-sm text-amber-900">
          <p className="font-medium">{tD('unverifiedTitle')}</p>
          <p>{tD('unverifiedBody')}</p>
        </div>
        <div className="flex gap-2 pt-2">
          <Button type="button" variant="outline" onClick={onClose} className="flex-1">
            {tD('done')}
          </Button>
          <Button
            type="button"
            disabled={verificationBlocked}
            onClick={handleRetry}
            className="flex-1"
          >
            {tD('retry')}
          </Button>
        </div>
      </div>
    );
  }

  return (
    <form onSubmit={handleSubmit(handleConnect)} className="space-y-4">
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
          {...register('link')}
          aria-invalid={!!error}
          aria-describedby={error ? 'yandex-connect-error' : undefined}
          placeholder={tD('linkPlaceholder')}
          className="font-mono text-xs"
        />
        <p className="text-xs text-ink-soft">{tD('linkHint')}</p>
      </div>

      {error && (
        <p id="yandex-connect-error" role="alert" className="text-sm text-destructive">
          {error}
        </p>
      )}
      <div className="flex gap-2 pt-2">
        <Button type="button" variant="outline" onClick={onClose} className="flex-1">
          {tD('cancel')}
        </Button>
        <Button type="submit" disabled={!link.trim() || verificationBlocked} className="flex-1">
          {tD('connect')}
        </Button>
      </div>
    </form>
  );
}
