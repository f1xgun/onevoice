import type { LucideIcon } from 'lucide-react';
import { cn } from '@/lib/utils';

export interface StatusLineProps {
  role: 'status' | 'alert';
  text: string;
  icon: LucideIcon;
  tone?: 'neutral' | 'success' | 'warning' | 'danger' | 'info';
}

const tones = {
  neutral: 'text-ink-soft',
  success: 'text-success',
  warning: 'text-warning',
  danger: 'text-danger',
  info: 'text-info',
};

export function StatusLine({ role, text, icon: Icon, tone = 'neutral' }: StatusLineProps) {
  return (
    <p role={role} className={cn('flex min-w-0 items-start gap-2 text-meta', tones[tone])}>
      <Icon aria-hidden="true" className="h-5 w-5 shrink-0" />
      <span className="min-w-0 break-words">{text}</span>
    </p>
  );
}
