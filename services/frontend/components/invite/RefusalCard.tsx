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
      <div className="flex flex-col gap-4">
        <h2 className="text-2xl font-semibold leading-8 text-ink">{tErrors('gone.title')}</h2>
        <p className="text-[15px] leading-[22px] text-ink-soft">{tErrors('gone.body')}</p>
        <Link href="/chat" className={cn(buttonVariants({ variant: 'secondary' }), 'mt-2 w-full')}>
          {tErrors('gone.cta')}
        </Link>
      </div>
    );
  }

  if (status === HTTP_STATUS.CONFLICT && businessId && businessName) {
    return (
      <div className="flex flex-col gap-4">
        <h2 className="text-2xl font-semibold leading-8 text-ink">
          {tErrors('alreadyMember.title')}
        </h2>
        <p className="text-[15px] leading-[22px] text-ink-soft">{tErrors('alreadyMember.body')}</p>
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
    <div className="flex flex-col gap-4">
      <p className="text-[15px] leading-[22px] text-ink-soft">{tErrors('generic')}</p>
    </div>
  );
}
