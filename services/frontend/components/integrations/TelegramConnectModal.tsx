'use client';

import { useTranslations } from 'next-intl';

import { toast } from 'sonner';
import { SheetContent } from '@/components/design-system/AppSheet';
import { PlatformIcon } from './PlatformIcons';
import { useTelegramConnectForm } from '@/hooks/useTelegramConnectForm';
import { ActionButton as Button } from '@/components/design-system/ActionButton';
import { AppInput as Input } from '@/components/design-system/AppInput';
import {
  Dialog,
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
  const tV2 = useTranslations('integrations.v2');
  const { register, handleSubmit, channelId, loading, error, handleConnect, handleClose } =
    useTelegramConnectForm(onClose);

  async function copyBotName() {
    try {
      await navigator.clipboard.writeText(tIntegrations('telegramBot'));
      toast.success(tV2('botCopied'));
    } catch {
      toast.error(tV2('copyFailed'));
    }
  }
  return (
    <Dialog open={open} onOpenChange={(v) => !v && handleClose()}>
      <SheetContent side="bottom" className="!pt-4">
        <DialogHeader className="pr-12 text-left">
          <DialogTitle className="flex items-center gap-3 text-block-title">
            <PlatformIcon platform="telegram" className="h-head-sm w-head-sm" />
            {tV2('telegramTitle')}
          </DialogTitle>
          <DialogDescription className="text-[15px] leading-[22px] text-ink-soft">
            {tIntegrations('telegramStep1Intro')}
          </DialogDescription>
        </DialogHeader>
        <form onSubmit={handleSubmit(handleConnect)} className="mt-4 space-y-4">
          <ol className="list-inside list-decimal space-y-4 text-reading">
            <li>
              {tIntegrations('telegramStep1Item1')}. {tIntegrations('telegramStep1Item2')}
            </li>
            <li>
              {tIntegrations('telegramStep1Item3Prefix')}{' '}
              <span className="font-medium">{tIntegrations('telegramBot')}</span>.{' '}
              {tIntegrations('telegramStep1Item4')}
              <Button
                type="button"
                variant="secondary"
                className="ml-5 mt-3"
                onClick={() => void copyBotName()}
              >
                {tV2('copyBot')}
              </Button>
            </li>
            <li>{tV2('telegramChannelStep')}</li>
          </ol>
          <label htmlFor="telegram-channel" className="block text-meta text-ink-soft">
            {tIntegrations('telegramStep2Placeholder')}
          </label>
          <Input
            id="telegram-channel"
            placeholder={tIntegrations('telegramStep2Placeholder')}
            aria-invalid={!!error}
            aria-describedby={error ? 'telegram-connect-error' : undefined}
            {...register('channelId')}
          />
          {error && (
            <p id="telegram-connect-error" role="alert" className="text-meta text-danger">
              {error}
            </p>
          )}
          <Button type="submit" disabled={!channelId.trim() || loading} className="w-full">
            {loading ? tIntegrations('telegramChecking') : tV2('verifyAndConnect')}
          </Button>
          <p className="text-meta text-ink-soft">{tV2('telegramNote')}</p>
        </form>
      </SheetContent>
    </Dialog>
  );
}
