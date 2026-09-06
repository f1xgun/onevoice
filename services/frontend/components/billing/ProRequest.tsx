'use client';

import { useState } from 'react';
import { useTranslations } from 'next-intl';
import { Button } from '@/components/ui/button';
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
} from '@/components/ui/dialog';
import { WaitlistForm } from '@/components/landing/WaitlistForm';

interface ProRequestProps {
  source: 'billing' | 'business-limit';
}

export function ProRequest({ source }: ProRequestProps) {
  const t = useTranslations('settings.billing.pro');
  const [open, setOpen] = useState(false);
  return (
    <>
      <Button type="button" onClick={() => setOpen(true)}>
        {t('cta')}
      </Button>
      <Dialog open={open} onOpenChange={setOpen}>
        <DialogContent className="max-h-[90vh] overflow-y-auto">
          <DialogHeader>
            <DialogTitle>{t('title')}</DialogTitle>
            <DialogDescription>{t('description')}</DialogDescription>
          </DialogHeader>
          <WaitlistForm source={source} plan="pro" submitLabel={t('cta')} />
        </DialogContent>
      </Dialog>
    </>
  );
}
