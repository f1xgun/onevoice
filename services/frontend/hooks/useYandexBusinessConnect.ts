'use client';

import { useState } from 'react';
import { useTranslations } from 'next-intl';
import { toast } from 'sonner';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { useForm } from 'react-hook-form';
import { zodResolver } from '@hookform/resolvers/zod';
import { z } from 'zod';

import { bizApi } from '@/lib/api/business-api';
import { INTEGRATION_ENDPOINTS } from '@/lib/constants/bizApiPaths';
import { QUERY_KEYS } from '@/lib/constants/queryKeys';
import { useBusinessStore } from '@/lib/stores/business';
import { STALE_TIME_5_MIN } from '@/lib/constants/cacheTTL';
import {
  isEmailVerificationRequiredError,
  useMapEmailVerificationError,
} from '@/lib/resolveErrorMap';

interface DelegatedConfig {
  available: boolean;
  rep_login: string;
}

export interface DelegatedFlowProps {
  activeBusinessId: string | null;
  repLogin: string;
  onClose: () => void;
}

type DelegatedStep = 'intro' | 'working' | 'unverified';
const COPIED_RESET_MS = 2000;
const delegatedSchema = z.object({ link: z.string().trim().min(1) });
type DelegatedFormData = z.infer<typeof delegatedSchema>;

export function useYandexBusinessConfig(open: boolean) {
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

  const delegatedAvailable = !!config?.available && !!config.rep_login;

  return {
    activeBusinessId,
    config,
    configLoading,
    configError,
    delegatedAvailable,
    mapVerifyError,
  };
}

export function useYandexDelegatedForm({
  activeBusinessId,
  repLogin,
  onClose,
}: DelegatedFlowProps) {
  const tD = useTranslations('integrations.yandexBusiness.delegated');
  const qc = useQueryClient();
  const mapVerifyError = useMapEmailVerificationError();
  const [step, setStep] = useState<DelegatedStep>('intro');
  const [error, setError] = useState<string | null>(null);
  const [verificationBlocked, setVerificationBlocked] = useState(false);
  const { register, watch, handleSubmit } = useForm<DelegatedFormData>({
    resolver: zodResolver(delegatedSchema),
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

  return {
    step,
    error,
    verificationBlocked,
    register,
    handleSubmit,
    link,
    copied,
    copyRepLogin,
    handleConnect,
    handleRetry,
  };
}
