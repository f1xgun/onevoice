'use client';

import { useTranslations } from 'next-intl';
import { Check, Copy, Loader2 } from 'lucide-react';

import { ActionButton as Button } from '@/components/design-system/ActionButton';
import {
  Dialog,
  AppDialog as DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
} from '@/components/design-system/AppDialog';
import { AppInput as Input } from '@/components/design-system/AppInput';
import { useYandexBusinessConfig, useYandexDelegatedForm } from '@/hooks/useYandexBusinessConnect';
import type { DelegatedFlowProps } from '@/hooks/useYandexBusinessConnect';

const LINK_HINT_ID = 'yandex-link-hint';
const LINK_ERROR_IDS = 'yandex-link-hint yandex-connect-error';

interface Props {
  open: boolean;
  onClose: () => void;
}

export function YandexBusinessConnectModal({ open, onClose }: Props) {
  const tYa = useTranslations('integrations.yandexBusiness');
  const {
    activeBusinessId,
    config,
    configLoading,
    configError,
    delegatedAvailable,
    mapVerifyError,
  } = useYandexBusinessConfig(open);
  const handleClose = onClose;

  return (
    <Dialog open={open} onOpenChange={(v) => !v && handleClose()}>
      <DialogContent className="max-w-2xl">
        <DialogHeader>
          <DialogTitle>{tYa('title')}</DialogTitle>
          <DialogDescription>{tYa('security')}</DialogDescription>
        </DialogHeader>

        {configLoading ? (
          <div className="flex justify-center py-12">
            <Loader2 className="h-8 w-8 animate-spin text-ink-soft" />
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

function DelegatedFlow({ activeBusinessId, repLogin, onClose }: DelegatedFlowProps) {
  const tD = useTranslations('integrations.yandexBusiness.delegated');
  const {
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
  } = useYandexDelegatedForm({ activeBusinessId, repLogin, onClose });

  if (step === 'working') {
    return (
      <div className="flex flex-col items-center gap-4 py-12">
        <Loader2 className="h-10 w-10 animate-spin text-ink-soft" />
        <div className="text-center">
          <p className="font-medium text-ink">{tD('working')}</p>
          <p className="mt-1 text-sm text-ink-soft">{tD('workingBody')}</p>
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
        <div className="space-y-2 rounded-lg bg-paper-raised p-4 text-sm text-warning">
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
              <Check className="mr-1 h-3.5 w-3.5 text-success" />
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
          aria-describedby={error ? LINK_ERROR_IDS : LINK_HINT_ID}
          placeholder={tD('linkPlaceholder')}
          className="font-mono"
        />
        <p id="yandex-link-hint" className="text-meta text-ink-soft">
          {tD('linkHint')}
        </p>
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
