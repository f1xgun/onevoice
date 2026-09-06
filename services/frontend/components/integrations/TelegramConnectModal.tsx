'use client';

import { useTranslations } from 'next-intl';

import { useTelegramConnectForm } from '@/hooks/useTelegramConnectForm';
import { ActionButton as Button } from '@/components/design-system/ActionButton';
import { AppInput as Input } from '@/components/design-system/AppInput';
import {
  Dialog,
  AppDialog as DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
} from '@/components/design-system/AppDialog';

interface Props {
  open: boolean;
  onClose: () => void;
}

export function TelegramConnectModal({ open, onClose }: Props) {
  const tIntegrations = useTranslations('integrations');
  const tCommon = useTranslations('common');
  const {
    step,
    setStep,
    register,
    handleSubmit,
    channelId,
    loading,
    error,
    handleConnect,
    handleClose,
  } = useTelegramConnectForm(onClose);

  return (
    <Dialog open={open} onOpenChange={(v) => !v && handleClose()}>
      <DialogContent className="max-w-md">
        <DialogHeader>
          <DialogTitle>{tIntegrations('telegramConnectModalTitle')}</DialogTitle>
          <DialogDescription>{tIntegrations('telegramStep1Intro')}</DialogDescription>
        </DialogHeader>

        {step === 1 && (
          <div className="space-y-4">
            <p className="text-sm text-ink-soft">{tIntegrations('telegramStep1Intro')}</p>
            <ol className="list-inside list-decimal space-y-2 text-sm text-ink-soft">
              <li>{tIntegrations('telegramStep1Item1')}</li>
              <li>{tIntegrations('telegramStep1Item2')}</li>
              <li>
                {tIntegrations('telegramStep1Item3Prefix')}{' '}
                <code className="rounded bg-paper-sunken px-1">{tIntegrations('telegramBot')}</code>
              </li>
              <li>{tIntegrations('telegramStep1Item4')}</li>
            </ol>
            <Button className="w-full" onClick={() => setStep(2)}>
              {tCommon('next')}
            </Button>
          </div>
        )}

        {step === 2 && (
          <form onSubmit={handleSubmit(handleConnect)} className="space-y-4">
            <p className="text-sm text-ink-soft">
              {tIntegrations('telegramStep2IntroBefore')}{' '}
              <code className="rounded bg-paper-sunken px-1">
                {tIntegrations('telegramExampleChannel')}
              </code>{' '}
              {tIntegrations('telegramStep2IntroOr')}{' '}
              <code className="rounded bg-paper-sunken px-1">-1001234567890</code>
              {tIntegrations('telegramStep2IntroAfter')}
            </p>
            <label htmlFor="telegram-channel" className="block text-meta font-medium">
              {tIntegrations('telegramStep2Placeholder')}
            </label>
            <Input
              id="telegram-channel"
              placeholder={tIntegrations('telegramStep2Placeholder')}
              aria-label={tIntegrations('telegramStep2Placeholder')}
              aria-invalid={!!error}
              aria-describedby={error ? 'telegram-connect-error' : undefined}
              {...register('channelId')}
            />
            {error && (
              <p id="telegram-connect-error" role="alert" className="text-sm text-destructive">
                {error}
              </p>
            )}
            <div className="flex gap-2">
              <Button type="button" variant="outline" onClick={() => setStep(1)} className="flex-1">
                {tCommon('back')}
              </Button>
              <Button type="submit" disabled={!channelId.trim() || loading} className="flex-1">
                {loading ? tIntegrations('telegramChecking') : tIntegrations('telegramSubmit')}
              </Button>
            </div>
          </form>
        )}
      </DialogContent>
    </Dialog>
  );
}
