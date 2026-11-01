'use client';

import { useEffect, useId, useRef } from 'react';
import { useMutation } from '@tanstack/react-query';
import { useTranslations } from 'next-intl';
import { bizApi } from '@/lib/api/business-api';
import { useBusinessStore } from '@/lib/stores/business';
import type { Attachment } from '@/types/chat';

interface ChatPhotoInputProps {
  disabled?: boolean;
  onChange: (attachment: Attachment) => void;
  onUploadingChange?: (uploading: boolean) => void;
}

const ACCEPTED_TYPES = 'image/jpeg,image/png,image/webp';
export function ChatPhotoInput({ disabled, onChange, onUploadingChange }: ChatPhotoInputProps) {
  const t = useTranslations('chat.instructions');
  const id = useId();
  const businessId = useBusinessStore((s) => s.activeBusinessId);
  const mounted = useRef(true);
  useEffect(() => {
    mounted.current = true;
    return () => {
      mounted.current = false;
    };
  }, []);
  const currentBusiness = useRef(businessId);
  currentBusiness.current = businessId;
  const upload = useMutation({
    onMutate: () => onUploadingChange?.(true),
    onSettled: () => {
      if (mounted.current) onUploadingChange?.(false);
    },
    mutationFn: async ({ file, scope }: { file: File; scope: string }) => {
      const data = new FormData();
      data.append('file', file);
      return (
        await bizApi(scope).post<Attachment>('/media', data, {
          headers: { 'Content-Type': 'multipart/form-data' },
        })
      ).data;
    },
    onSuccess: (attachment, { scope }) => {
      if (
        mounted.current &&
        currentBusiness.current === scope &&
        useBusinessStore.getState().activeBusinessId === scope
      )
        onChange(attachment);
    },
  });
  return (
    <div>
      <label
        htmlFor={id}
        className="flex min-h-11 cursor-pointer items-center justify-center rounded-inner bg-btn2 px-3 text-sm font-medium text-ink"
      >
        {upload.isPending ? t('uploading') : t('photo')}
      </label>
      <input
        id={id}
        className="sr-only"
        type="file"
        accept={ACCEPTED_TYPES}
        disabled={disabled || upload.isPending || !businessId}
        onChange={(event) => {
          const file = event.target.files?.[0];
          if (file && businessId) upload.mutate({ file, scope: businessId });
          event.target.value = '';
        }}
      />
      {upload.isError && (
        <p role="alert" className="mt-1 text-sm text-danger">
          {t('uploadError')}
        </p>
      )}
    </div>
  );
}
