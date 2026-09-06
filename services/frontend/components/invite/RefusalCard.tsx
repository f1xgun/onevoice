'use client';

import Link from 'next/link';
import { useTranslations } from 'next-intl';
import { ActionButton as Button } from '@/components/design-system/ActionButton';
import { actionButtonVariants as buttonVariants } from '@/components/design-system/action-button-variants';
import { cn } from '@/lib/utils';
import { useBusinessStore } from '@/lib/stores/business';
import { HTTP_STATUS } from '@/lib/constants/httpStatus';

export interface RefusalCardProps {
  status: number;
  businessId?: string;
  businessName?: string;
  onOpenBusiness?: () => void;
}

export function RefusalCard({
  status,
  businessId,
  businessName,
  onOpenBusiness,
}: RefusalCardProps) {
  const tErrors = useTranslations('invite.accept.errors');
  const setActive = useBusinessStore((s) => s.setActive);

  if (status === HTTP_STATUS.GONE) {
    return (
      <div className="flex flex-col gap-4 text-center">
        <h2 className="text-lg font-medium tracking-tight text-ink">{tErrors('gone.title')}</h2>
        <p className="text-sm text-ink-mid">{tErrors('gone.body')}</p>
        <Link href="/chat" className={cn(buttonVariants({ variant: 'secondary' }), 'mt-2 w-full')}>
          {tErrors('gone.cta')}
        </Link>
      </div>
    );
  }

  if (status === HTTP_STATUS.CONFLICT && businessId && businessName) {
    return (
      <div className="flex flex-col gap-4 text-center">
        <h2 className="text-lg font-medium tracking-tight text-ink">
          {tErrors('alreadyMember.title')}
        </h2>
        <p className="text-sm text-ink-mid">{tErrors('alreadyMember.body')}</p>
        <Button
          variant="default"
          className="mt-2 w-full"
          onClick={() => {
            setActive(businessId);
            onOpenBusiness?.();
          }}
        >
          {tErrors('alreadyMember.cta', { businessName })}
        </Button>
      </div>
    );
  }

  return (
    <div className="flex flex-col gap-4 text-center">
      <p className="text-sm text-ink-mid">{tErrors('generic')}</p>
    </div>
  );
}
