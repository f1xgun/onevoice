'use client';

import { useState } from 'react';
import { useTranslations } from 'next-intl';
import { toast } from 'sonner';
import { api } from '@/lib/api';
import { API_PATHS } from '@/lib/constants/apiPaths';
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
  const [step, setStep] = useState(1);
  const [channelId, setChannelId] = useState('');
  const [loading, setLoading] = useState(false);

  const handleConnect = async () => {
    if (!channelId.trim()) return;
    setLoading(true);
    try {
      await api.post(API_PATHS.INTEGRATIONS.TELEGRAM_CONNECT, {
        channel_id: channelId.trim(),
      });
      toast.success(tIntegrations('telegramConnected'));
      handleClose();
    } catch {
      toast.error(tIntegrations('telegramConnectFail'));
    } finally {
      setLoading(false);
    }
  };

  const handleClose = () => {
    setStep(1);
    setChannelId('');
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
                <code className="rounded bg-gray-100 px-1">@OneVoiceBot</code>
              </li>
              <li>{tIntegrations('telegramStep1Item4')}</li>
            </ol>
            <Button className="w-full" onClick={() => setStep(2)}>
              {tCommon('next')}
            </Button>
          </div>
        )}

        {step === 2 && (
          <div className="space-y-4">
            <p className="text-sm text-gray-600">
              {tIntegrations('telegramStep2IntroBefore')}{' '}
              <code className="rounded bg-gray-100 px-1">@mychannel</code>{' '}
              {tIntegrations('telegramStep2IntroOr')}{' '}
              <code className="rounded bg-gray-100 px-1">-1001234567890</code>
              {tIntegrations('telegramStep2IntroAfter')}
            </p>
            <Input
              placeholder={tIntegrations('telegramStep2Placeholder')}
              value={channelId}
              onChange={(e) => setChannelId(e.target.value)}
            />
            <div className="flex gap-2">
              <Button variant="outline" onClick={() => setStep(1)} className="flex-1">
                {tCommon('back')}
              </Button>
              <Button
                onClick={handleConnect}
                disabled={!channelId.trim() || loading}
                className="flex-1"
              >
                {loading ? tIntegrations('telegramChecking') : tIntegrations('telegramSubmit')}
              </Button>
            </div>
          </div>
        )}
      </DialogContent>
    </Dialog>
  );
}
