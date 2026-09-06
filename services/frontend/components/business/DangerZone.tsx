'use client';

import { useState } from 'react';
import { useTranslations } from 'next-intl';

import { ActionButton as Button } from '@/components/design-system/ActionButton';
import { Card } from '@/components/ui/card';
import { useBusinessList } from '@/lib/hooks/useBusinessList';
import { DeleteBusinessModal } from './DeleteBusinessModal';

const OWNER_ROLE_ID = '00000000-0000-0000-0000-000000000001';

interface DangerZoneProps {
  businessId: string;
  businessName: string;
}

/**
 * Owner-only «Опасная зона» for the organization profile page. Mirrors the
 * account Danger Zone. Renders nothing unless the caller holds the system OWNER
 * role in this organization (the backend re-checks; this is UX-only). Opens
 * DeleteBusinessModal, passing the caller's other memberships for post-delete
 * routing.
 */
export function DangerZone({ businessId, businessName }: DangerZoneProps) {
  const t = useTranslations('business.deletion');
  const [confirmOpen, setConfirmOpen] = useState(false);
  const { data: memberships } = useBusinessList();

  const current = memberships?.find((m) => m.id === businessId);
  const isOwner = current?.role.id === OWNER_ROLE_ID;
  if (!isOwner) return null;

  const otherBusinessIds = (memberships ?? []).filter((m) => m.id !== businessId).map((m) => m.id);

  return (
    <Card className="border-[var(--ov-danger)]/40 p-8">
      <h2 className="text-[22px] font-medium leading-[1.2] tracking-[-0.015em]">
        {t('dangerZoneHeading')}
      </h2>
      <p className="mt-3 text-[15px] leading-[1.55] text-[var(--ov-ink-mid)]">
        {t('dangerZoneDescription')}
      </p>
      <Button variant="danger" className="mt-5" onClick={() => setConfirmOpen(true)}>
        {t('deleteButton')}
      </Button>

      <DeleteBusinessModal
        open={confirmOpen}
        onOpenChange={setConfirmOpen}
        businessId={businessId}
        businessName={businessName}
        otherBusinessIds={otherBusinessIds}
      />
    </Card>
  );
}
