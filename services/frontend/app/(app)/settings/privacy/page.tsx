'use client';

import { useState } from 'react';
import { useTranslations } from 'next-intl';
import { WithdrawalPanel } from '@/components/legal/WithdrawalPanel';
import { DataControllerBlock } from '@/components/legal/DataControllerBlock';
import { DeleteBusinessModal } from '@/components/business/DeleteBusinessModal';
import { ActionButton } from '@/components/design-system/ActionButton';
import { useBusinessStore } from '@/lib/stores/business';
import { useBusinessList } from '@/lib/hooks/useBusinessList';
import { AuditSection } from '../audit/_components/AuditSection';

const OWNER_ROLE_ID = '00000000-0000-0000-0000-000000000001';

export default function PrivacySettingsPage() {
  const t = useTranslations('settings.v2');
  const tDelete = useTranslations('business.deletion');
  const businessId = useBusinessStore((s) => s.activeBusinessId);
  const { data: memberships } = useBusinessList();
  const current = memberships?.find((m) => m.id === businessId);
  const isOwner = current?.role.id === OWNER_ROLE_ID;
  const [confirmOpen, setConfirmOpen] = useState(false);
  return (
    <div className="mx-auto max-w-[816px] space-y-group px-gutter pb-10 pt-5 md:px-gutter-desktop">
      <h1 className="text-page-title">{t('data')}</h1>
      <WithdrawalPanel />
      <AuditSection embedded />
      {current && businessId && (
        <section>
          <h2 className="text-group-title">{t('deleteLocation')}</h2>
          <p className="mt-1 text-[15px] leading-[22px] text-ink-soft">
            {tDelete('dangerZoneDescription')}
          </p>
          <ActionButton
            variant="danger"
            disabled={!isOwner}
            aria-describedby={!isOwner ? 'owner-only-delete' : undefined}
            className="mt-4"
            onClick={() => setConfirmOpen(true)}
          >
            {tDelete('deleteButton')}
          </ActionButton>
          {!isOwner && (
            <p id="owner-only-delete" className="mt-2 text-meta text-ink-soft">
              {tDelete('errors.notOwner')}
            </p>
          )}
          {isOwner && (
            <DeleteBusinessModal
              open={confirmOpen}
              onOpenChange={setConfirmOpen}
              businessId={businessId}
              businessName={current.name}
              otherBusinessIds={(memberships ?? [])
                .filter((m) => m.id !== businessId)
                .map((m) => m.id)}
            />
          )}
        </section>
      )}
      <details className="text-meta text-ink-soft">
        <summary className="min-h-tap cursor-pointer">{t('controller')}</summary>
        <DataControllerBlock />
      </details>
    </div>
  );
}
