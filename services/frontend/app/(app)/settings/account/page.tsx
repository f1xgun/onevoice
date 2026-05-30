// Surface 7: /settings/account "Опасная зона"
// section. Single Danger Zone card with the «Удалить аккаунт» button
// that opens DeleteConfirmModal (Surface 8). The full flow:
// DeleteConfirmModal → on 409 → SoleOwnerBlockedModal (Surface 9)
// → on 204 → redirect /login + success toast
//
// Verbatim RU copy via i18n keys under account.deletion.* (which are
// also referenced by DeletionGraceBanner / DeleteConfirmModal /
// SoleOwnerBlockedModal).

'use client';

import { useState } from 'react';
import { useTranslations } from 'next-intl';
import { Button } from '@/components/ui/button';
import { Card } from '@/components/ui/card';
import { DeleteConfirmModal } from '@/components/account/DeleteConfirmModal';

export default function AccountSettingsPage() {
  const t = useTranslations('account.deletion');
  const [confirmOpen, setConfirmOpen] = useState(false);

  return (
    <div className="mx-auto max-w-[640px] space-y-12 p-6 md:p-12">
      <Card className="border-[var(--ov-danger)]/40 p-8">
        <h2 className="text-[28px] font-medium leading-[1.2] tracking-[-0.015em]">
          {t('dangerZoneHeading')}
        </h2>
        <p className="mt-4 text-[15px] leading-[1.55] text-[var(--ov-ink-mid)]">
          {t('dangerZoneDescription')}
        </p>
        <Button variant="danger" className="mt-6" onClick={() => setConfirmOpen(true)}>
          {t('deleteButton')}
        </Button>
      </Card>

      <DeleteConfirmModal open={confirmOpen} onOpenChange={setConfirmOpen} />
    </div>
  );
}
