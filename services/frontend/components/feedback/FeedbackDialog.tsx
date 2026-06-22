'use client';

import { useEffect } from 'react';
import { usePathname } from 'next/navigation';
import { useTranslations } from 'next-intl';
import { useForm } from 'react-hook-form';
import { zodResolver } from '@hookform/resolvers/zod';
import { z } from 'zod';
import { toast } from 'sonner';

import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog';
import { Button } from '@/components/ui/button';
import { Label } from '@/components/ui/label';
import { Textarea } from '@/components/ui/textarea';
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select';
import { useSubmitFeedback } from '@/lib/hooks/useFeedback';
import type { FeedbackCategory } from '@/lib/api/feedback';

const FEEDBACK_MESSAGE_MAX_LEN = 2000;
const CATEGORIES: readonly FeedbackCategory[] = ['bug', 'idea', 'question', 'other'];

export interface FeedbackDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
}

export function FeedbackDialog({ open, onOpenChange }: FeedbackDialogProps) {
  const t = useTranslations('feedback');
  const tV = useTranslations('validation');
  const pathname = usePathname();
  const submit = useSubmitFeedback();

  const schema = z.object({
    category: z.enum(['bug', 'idea', 'question', 'other']),
    message: z
      .string()
      .min(1, tV('feedbackMessageRequired'))
      .max(FEEDBACK_MESSAGE_MAX_LEN, tV('maxChars', { count: FEEDBACK_MESSAGE_MAX_LEN })),
  });
  type FormInput = z.infer<typeof schema>;

  const {
    register,
    handleSubmit,
    setValue,
    watch,
    reset,
    formState: { errors, isSubmitting },
  } = useForm<FormInput>({
    resolver: zodResolver(schema),
    defaultValues: { category: 'idea', message: '' },
  });

  useEffect(() => {
    if (!open) {
      reset({ category: 'idea', message: '' });
    }
  }, [open, reset]);

  const category = watch('category');

  async function onSubmit(data: FormInput) {
    try {
      await submit.mutateAsync({
        category: data.category,
        message: data.message,
        page: pathname,
      });
      toast.success(t('successToast'));
      onOpenChange(false);
    } catch {
      toast.error(t('errorToast'));
    }
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-md rounded-lg border border-line bg-paper-raised p-6 shadow-ov-2">
        <DialogHeader>
          <DialogTitle>{t('title')}</DialogTitle>
          <DialogDescription>{t('description')}</DialogDescription>
        </DialogHeader>
        <form onSubmit={handleSubmit(onSubmit)} className="mt-4 flex flex-col gap-4">
          <div className="flex flex-col gap-1.5">
            <Label className="text-xs font-medium text-ink-mid">{t('categoryLabel')}</Label>
            <Select
              value={category}
              onValueChange={(v) => setValue('category', v as FeedbackCategory)}
            >
              <SelectTrigger>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {CATEGORIES.map((c) => (
                  <SelectItem key={c} value={c}>
                    {t(`category.${c}`)}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>

          <div className="flex flex-col gap-1.5">
            <Label htmlFor="feedback-message" className="text-xs font-medium text-ink-mid">
              {t('messageLabel')}
            </Label>
            <Textarea
              id="feedback-message"
              rows={5}
              placeholder={t('messagePlaceholder')}
              {...register('message')}
            />
            {errors.message && (
              <p className="text-sm text-danger" role="alert">
                {errors.message.message}
              </p>
            )}
          </div>

          <DialogFooter className="mt-2 flex justify-end gap-2">
            <Button variant="ghost" type="button" onClick={() => onOpenChange(false)}>
              {t('cancel')}
            </Button>
            <Button type="submit" disabled={isSubmitting || submit.isPending}>
              {submit.isPending ? t('submitting') : t('submit')}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
