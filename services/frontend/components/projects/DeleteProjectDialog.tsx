'use client';

import { useState } from 'react';
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
import { chatsPluralRu } from '@/lib/plural';

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
  const [pending, setPending] = useState(false);

  const description =
    chatCount > 0
      ? `Будет также удалено ${chatCount} ${chatsPluralRu(chatCount)}. Это действие нельзя отменить.`
      : 'Проект будет удалён. Это действие нельзя отменить.';

  const handleConfirm = async () => {
    setPending(true);
    try {
      await onConfirm();
      onOpenChange(false);
    } catch {
      toast.error('Не удалось удалить проект', {
        description: 'Попробуйте ещё раз.',
      });
    } finally {
      setPending(false);
    }
  };

  return (
    <AlertDialog open={open} onOpenChange={onOpenChange}>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>{`Удалить проект «${projectName}»?`}</AlertDialogTitle>
          <AlertDialogDescription>{description}</AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel disabled={pending}>Отмена</AlertDialogCancel>
          <AlertDialogAction
            className="bg-destructive text-destructive-foreground hover:bg-destructive/90"
            disabled={pending}
            onClick={(e) => {
              // Prevent default close so we can await the mutation.
              e.preventDefault();
              void handleConfirm();
            }}
          >
            {pending ? 'Удаляем…' : 'Удалить'}
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  );
}
