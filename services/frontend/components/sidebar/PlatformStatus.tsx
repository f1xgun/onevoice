'use client';

import { AlertCircle, Check, HelpCircle, Loader2, Minus } from 'lucide-react';
import { useTranslations } from 'next-intl';

import { PlatformIcon } from '@/components/integrations/PlatformIcons';
import { cn } from '@/lib/utils';

import { connectionStatus, type ConnectionState } from './connectionStatus';

interface PlatformStatusProps extends ConnectionState {
  label: string;
  platform: string;
}

const STATUS_ICONS = {
  chooseOrganization: Minus,
  connectionLoading: Loader2,
  connectionUnknown: HelpCircle,
  connected: Check,
  notConnected: Minus,
  connectionAttention: AlertCircle,
};

export function PlatformStatus({ label, platform, ...state }: PlatformStatusProps) {
  const tNav = useTranslations('nav');
  const status = connectionStatus(state);
  const Icon = STATUS_ICONS[status];
  return (
    <span
      className={cn(
        'grid grid-cols-[18px_minmax(0,1fr)_14px] items-center gap-2 px-3 text-meta',
        status === 'connectionAttention' ? 'text-danger' : 'text-ink-soft'
      )}
    >
      <PlatformIcon platform={platform} className="h-[18px] w-[18px]" />
      <span>
        {label}: {tNav(status)}
      </span>
      <Icon size={14} className="shrink-0" aria-hidden />
    </span>
  );
}
