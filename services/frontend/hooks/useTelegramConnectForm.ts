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

const telegramSchema = z.object({ channelId: z.string().trim().min(1) });
type TelegramFormData = z.infer<typeof telegramSchema>;

export function useTelegramConnectForm(onClose: () => void) {
  const tIntegrations = useTranslations('integrations');
  const mapVerifyError = useMapEmailVerificationError();
  const [step, setStep] = useState(1);
  const tErrors = useTranslations('integrations.telegramErrors');
  const mapConnectError = createMapTelegramConnectError(tErrors);
  const [error, setError] = useState<string | null>(null);
  const { register, watch, reset, setFocus, handleSubmit } = useForm<TelegramFormData>({
    resolver: zodResolver(telegramSchema),
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

  return {
    step,
    setStep,
    register,
    handleSubmit,
    channelId,
    loading,
    error,
    handleConnect,
    handleClose,
  };
}
