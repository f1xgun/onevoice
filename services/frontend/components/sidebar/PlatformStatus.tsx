'use client';

import { AlertCircle, Check, HelpCircle, Loader2, Minus } from 'lucide-react';
import { useTranslations } from 'next-intl';

import { cn } from '@/lib/utils';

import { connectionStatus, type ConnectionState } from './connectionStatus';

interface PlatformStatusProps extends ConnectionState {
  label: string;
}

const STATUS_ICONS = {
  chooseOrganization: Minus,
  connectionLoading: Loader2,
  connectionUnknown: HelpCircle,
  connected: Check,
  notConnected: Minus,
  connectionAttention: AlertCircle,
};

export function PlatformStatus({ label, ...state }: PlatformStatusProps) {
  const tNav = useTranslations('nav');
  const status = connectionStatus(state);
  const Icon = STATUS_ICONS[status];
  return (
    <span
      className={cn(
        'flex items-center gap-2 text-meta',
        status === 'connectionAttention' ? 'text-danger' : 'text-ink-soft'
      )}
    >
      <Icon size={18} className="shrink-0" aria-hidden />
      <span>
        {label}: {tNav(status)}
      </span>
    </span>
  );
}
