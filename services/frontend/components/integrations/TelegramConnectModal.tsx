'use client';

import { useState } from 'react';
import { useForm } from 'react-hook-form';
import { zodResolver } from '@hookform/resolvers/zod';
import { z } from 'zod';
import { useTranslations } from 'next-intl';
import { toast } from 'sonner';
import { bizApi } from '@/lib/api/business-api';
import { INTEGRATION_ENDPOINTS } from '@/lib/constants/bizApiPaths';
import { useBusinessStore } from '@/lib/stores/business';
import { createMapTelegramConnectError, useMapEmailVerificationError } from '@/lib/resolveErrorMap';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Dialog, DialogContent, DialogHeader, DialogTitle } from '@/components/ui/dialog';

interface Props {
  open: boolean;
  onClose: () => void;
}

export function TelegramConnectModal({ open, onClose }: Props) {
  const tIntegrations = useTranslations('integrations');
  const tCommon = useTranslations('common');
  const mapVerifyError = useMapEmailVerificationError();
  const [step, setStep] = useState(1);
  const tErrors = useTranslations('integrations.telegramErrors');
  const mapConnectError = createMapTelegramConnectError(tErrors);
  const [error, setError] = useState<string | null>(null);
  const { register, watch, reset, setFocus, handleSubmit } = useForm<{ channelId: string }>({
    resolver: zodResolver(z.object({ channelId: z.string().trim().min(1) })),
    defaultValues: { channelId: '' },
  });
  const channelId = watch('channelId');
  const [loading, setLoading] = useState(false);
  const activeBusinessId = useBusinessStore((s) => s.activeBusinessId);

  const handleConnect = async () => {
    if (!channelId.trim() || !activeBusinessId) return;
    const connectPath = INTEGRATION_ENDPOINTS.telegram?.connect;
    if (!connectPath) return;
    setError(null);
    setLoading(true);
    try {
      await bizApi(activeBusinessId).post(connectPath, {
        channel_id: channelId.trim(),
      });
      toast.success(tIntegrations('telegramConnected'));
      handleClose();
    } catch (err: unknown) {
      const message = mapVerifyError(err) ?? mapConnectError(err);
      setError(message);
      setFocus('channelId');
    } finally {
      setLoading(false);
    }
  };

  const handleClose = () => {
    setStep(1);
    reset();
    setError(null);
    onClose();
  };

  return (
    <Dialog open={open} onOpenChange={(v) => !v && handleClose()}>
      <DialogContent className="max-w-md">
        <DialogHeader>
          <DialogTitle>{tIntegrations('telegramConnectModalTitle')}</DialogTitle>
        </DialogHeader>

        {step === 1 && (
          <div className="space-y-4">
            <p className="text-sm text-gray-600">{tIntegrations('telegramStep1Intro')}</p>
            <ol className="list-inside list-decimal space-y-2 text-sm text-gray-600">
              <li>{tIntegrations('telegramStep1Item1')}</li>
              <li>{tIntegrations('telegramStep1Item2')}</li>
              <li>
                {tIntegrations('telegramStep1Item3Prefix')}{' '}
                <code className="rounded bg-gray-100 px-1">{tIntegrations('telegramBot')}</code>
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
            <p className="text-sm text-gray-600">
              {tIntegrations('telegramStep2IntroBefore')}{' '}
              <code className="rounded bg-gray-100 px-1">
                {tIntegrations('telegramExampleChannel')}
              </code>{' '}
              {tIntegrations('telegramStep2IntroOr')}{' '}
              <code className="rounded bg-gray-100 px-1">-1001234567890</code>
              {tIntegrations('telegramStep2IntroAfter')}
            </p>
            <Input
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
