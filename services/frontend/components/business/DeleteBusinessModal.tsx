'use client';

import { useState } from 'react';
import { useTranslations } from 'next-intl';
import { useRouter } from 'next/navigation';
import { useQueryClient } from '@tanstack/react-query';
import { toast } from 'sonner';

import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from '@/components/ui/alert-dialog';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { FieldError } from '@/components/ui/field-error';
import { Button } from '@/components/ui/button';
import { useBusinessStore } from '@/lib/stores/business';
import { useBusinessDeletionStore, DELETION_GRACE_MS } from '@/lib/stores/businessDeletion';
import { BUSINESS_LIST_QUERY_KEY } from '@/lib/hooks/useBusinessList';
import { deleteBusiness, type BusinessDeletionError } from '@/lib/api/business-deletion';

interface DeleteBusinessModalProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  businessId: string;
  businessName: string;
  /** ids of the caller's other memberships, used to route after deletion. */
  otherBusinessIds: string[];
}

/**
 * Confirmation modal for organization deletion. Mirrors DeleteConfirmModal but
 * gates on typing the organization name (organizations are not password-gated).
 * On 204: clears the active business, routes to another membership if one
 * exists, else /onboarding.
 */
export function DeleteBusinessModal({
  open,
  onOpenChange,
  businessId,
  businessName,
  otherBusinessIds,
}: DeleteBusinessModalProps) {
  const t = useTranslations('business.deletion.confirmModal');
  const tErrors = useTranslations('business.deletion.errors');
  const router = useRouter();
  const queryClient = useQueryClient();
  const setActive = useBusinessStore((s) => s.setActive);
  const setPendingDeletion = useBusinessDeletionStore((s) => s.setPending);

  const [confirmName, setConfirmName] = useState('');
  const [submitting, setSubmitting] = useState(false);
  const [fieldError, setFieldError] = useState<string | null>(null);

  const nameMatches = confirmName.trim() === businessName.trim();

  function reset() {
    setConfirmName('');
    setSubmitting(false);
    setFieldError(null);
  }

  async function handleSubmit() {
    if (!nameMatches) {
      setFieldError(t('nameMismatch'));
      return;
    }
    setFieldError(null);
    setSubmitting(true);
    try {
      await deleteBusiness(businessId);
      toast.success(t('successToast'));
      setPendingDeletion({
        id: businessId,
        name: businessName,
        scheduledDeletionAt: new Date(Date.now() + DELETION_GRACE_MS).toISOString(),
      });
      const next = otherBusinessIds[0] ?? null;
      setActive(next);
      await queryClient.invalidateQueries({ queryKey: BUSINESS_LIST_QUERY_KEY });
      router.push(next ? '/business' : '/onboarding');
    } catch (e) {
      const err = e as BusinessDeletionError;
      setSubmitting(false);
      switch (err.code) {
        case 'not_organization_owner':
          setFieldError(tErrors('notOwner'));
          return;
        case 'business_pending_deletion':
          toast.error(tErrors('pendingDeletion'));
          onOpenChange(false);
          window.location.reload();
          return;
        default:
          toast.error(tErrors('generic'));
      }
    }
  }

  return (
    <AlertDialog
      open={open}
      onOpenChange={(nextOpen) => {
        if (!nextOpen) reset();
        onOpenChange(nextOpen);
      }}
    >
      <AlertDialogContent className="max-w-[480px]">
        <AlertDialogHeader>
          <AlertDialogTitle className="text-[28px] font-medium leading-[1.2] tracking-[-0.015em]">
            {t('heading')}
          </AlertDialogTitle>
          <AlertDialogDescription className="text-[15px] leading-[1.55] text-[var(--ov-ink-mid)]">
            {t('body', { name: businessName })}
          </AlertDialogDescription>
        </AlertDialogHeader>

        <div className="my-4 space-y-2">
          <Label htmlFor="delete-business-confirm-name" className="text-sm font-medium">
            {t('nameLabel', { name: businessName })}
          </Label>
          <Input
            id="delete-business-confirm-name"
            type="text"
            autoComplete="off"
            value={confirmName}
            onChange={(e) => setConfirmName(e.target.value)}
            placeholder={businessName}
            disabled={submitting}
          />
          {fieldError ? <FieldError>{fieldError}</FieldError> : null}
        </div>

        <AlertDialogFooter>
          <AlertDialogCancel disabled={submitting}>{t('cancel')}</AlertDialogCancel>
          <AlertDialogAction
            asChild
            onClick={(e) => {
              e.preventDefault();
              void handleSubmit();
            }}
          >
            <Button variant="danger" disabled={submitting || !nameMatches}>
              {submitting ? t('submitting') : t('cta')}
            </Button>
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  );
}
