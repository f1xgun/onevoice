'use client';

import { useState } from 'react';
import { useTranslations } from 'next-intl';
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

interface DeleteProjectDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  projectName: string;
  chatCount: number;
  onConfirm: () => Promise<void>;
}

export function DeleteProjectDialog({
  open,
  onOpenChange,
  projectName,
  chatCount,
  onConfirm,
}: DeleteProjectDialogProps) {
  const tCommon = useTranslations('common');
  const tDelete = useTranslations('projects.deleteDialog');
  const [pending, setPending] = useState(false);

  const description =
    chatCount > 0
      ? tDelete('descriptionWithChats', { count: chatCount })
      : tDelete('descriptionEmpty');

  const handleConfirm = async () => {
    setPending(true);
    try {
      await onConfirm();
      onOpenChange(false);
    } catch {
      toast.error(tDelete('toastError'), {
        description: tDelete('toastRetry'),
      });
    } finally {
      setPending(false);
    }
  };

  return (
    <AlertDialog open={open} onOpenChange={onOpenChange}>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>{tDelete('title', { name: projectName })}</AlertDialogTitle>
          <AlertDialogDescription>{description}</AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel disabled={pending}>{tCommon('cancel')}</AlertDialogCancel>
          <AlertDialogAction
            className="hover:bg-destructive/90 bg-destructive text-destructive-foreground"
            disabled={pending}
            onClick={(e) => {
              e.preventDefault();
              void handleConfirm();
            }}
          >
            {pending ? tDelete('deleting') : tDelete('delete')}
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  );
}
