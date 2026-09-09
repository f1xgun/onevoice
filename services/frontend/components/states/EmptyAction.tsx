import Link from 'next/link';
import { useTranslations } from 'next-intl';
import { ActionButton } from '@/components/design-system/ActionButton';

interface EmptyActionProps {
  href?: string;
  action?: 'openChat' | 'gettingStarted' | 'connectChannels' | 'chooseBusiness';
}

export function EmptyAction({ href = '/chat', action = 'openChat' }: EmptyActionProps) {
  const t = useTranslations('states.actions');
  return (
    <ActionButton asChild size="sm">
      <Link href={href}>{t(action)}</Link>
    </ActionButton>
  );
}
