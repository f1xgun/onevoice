'use client';

// Linen rebuild — Phase 4.8.
// Renders the "Основное" section body (no card chrome, no heading — the page
// wraps each form in a paper-raised section). Save button is rendered by the
// form so the section stays self-contained; the page-level header save lives
// elsewhere and is reserved for cross-section coordination later.

import { useEffect, useRef, useState } from 'react';
import { useForm, Controller } from 'react-hook-form';
import { zodResolver } from '@hookform/resolvers/zod';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import Image from 'next/image';
import { toast } from 'sonner';
import { api } from '@/lib/api';
import { businessSchema, type BusinessInput } from '@/lib/schemas';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select';
import type { Business } from '@/types/business';

const CATEGORIES = [
  { value: 'cafe', label: 'Кафе / Ресторан' },
  { value: 'retail', label: 'Розничная торговля' },
  { value: 'service', label: 'Услуги' },
  { value: 'beauty', label: 'Красота и здоровье' },
  { value: 'education', label: 'Образование' },
  { value: 'other', label: 'Другое' },
];

export function ProfileForm({ defaultValues }: { defaultValues?: Partial<Business> }) {
  const qc = useQueryClient();
  const fileInputRef = useRef<HTMLInputElement>(null);
  const [logoUrl, setLogoUrl] = useState(defaultValues?.logoUrl ?? '');

  const {
    register,
    handleSubmit,
    control,
    reset,
    formState: { errors, isSubmitting, isDirty },
  } = useForm<BusinessInput>({
    resolver: zodResolver(businessSchema),
    defaultValues: defaultValues ?? {},
  });

  useEffect(() => {
    if (defaultValues) {
      reset(defaultValues);
      setLogoUrl(defaultValues.logoUrl ?? '');
    }
  }, [defaultValues, reset]);

  const mutation = useMutation({
    mutationFn: (data: BusinessInput) => api.put('/business', data),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['business'] });
      toast.success('Данные сохранены');
    },
    onError: () => toast.error('Не получилось сохранить'),
  });

  const logoMutation = useMutation({
    mutationFn: (file: File) => {
      const formData = new FormData();
      formData.append('logo', file);
      return api.put<Business>('/business/logo', formData, {
        headers: { 'Content-Type': 'multipart/form-data' },
      });
    },
    onSuccess: (res) => {
      setLogoUrl(res.data.logoUrl ?? '');
      qc.invalidateQueries({ queryKey: ['business'] });
      toast.success('Логотип обновлён');
    },
    onError: () => toast.error('Не получилось загрузить логотип'),
  });

  function handleFileChange(e: React.ChangeEvent<HTMLInputElement>) {
    const file = e.target.files?.[0];
    if (file) logoMutation.mutate(file);
    e.target.value = '';
  }

  return (
    <form onSubmit={handleSubmit((d) => mutation.mutate(d))} className="flex flex-col gap-5">
      {/* Logo */}
      <div className="flex items-center gap-5">
        <button
          type="button"
          onClick={() => fileInputRef.current?.click()}
          disabled={logoMutation.isPending}
          className="relative grid h-20 w-20 shrink-0 place-items-center overflow-hidden rounded-md border border-line bg-paper-sunken text-ink-soft transition-colors hover:border-ochre/40 hover:text-ink disabled:cursor-not-allowed disabled:opacity-60"
          aria-label="Загрузить логотип"
        >
          {logoUrl ? (
            <Image
              src={logoUrl}
              alt="Логотип"
              width={80}
              height={80}
              className="h-full w-full object-cover"
              unoptimized
            />
          ) : (
            <span className="font-mono text-[11px] uppercase tracking-[0.04em]">лого</span>
          )}
          {logoMutation.isPending && (
            <span className="absolute inset-0 grid place-items-center bg-paper/70 font-mono text-[11px] uppercase tracking-[0.04em] text-ink-soft">
              …
            </span>
          )}
        </button>
        <div className="min-w-0 flex-1">
          <div className="text-sm font-medium text-ink">Логотип</div>
          <p className="mt-0.5 text-[13px] text-ink-soft">
            JPEG, PNG, WebP — до 5 МБ. Квадратный, минимум 256 px.
          </p>
          <div className="mt-3 flex gap-2">
            <Button
              type="button"
              variant="secondary"
              size="sm"
              onClick={() => fileInputRef.current?.click()}
              disabled={logoMutation.isPending}
            >
              Загрузить
            </Button>
            {logoUrl && (
              <Button
                type="button"
                variant="ghost"
                size="sm"
                onClick={() => setLogoUrl('')}
                disabled={logoMutation.isPending}
              >
                Удалить
              </Button>
            )}
          </div>
        </div>
        <input
          ref={fileInputRef}
          type="file"
          accept="image/jpeg,image/png,image/webp,image/gif"
          className="hidden"
          onChange={handleFileChange}
        />
      </div>

      <div className="grid grid-cols-1 gap-4 md:grid-cols-2">
        <Field label="Название" required error={errors.name?.message}>
          <Input id="name" {...register('name')} placeholder="Кофейня «Мята»" />
        </Field>

        <Field label="Категория" required error={errors.category?.message}>
          <Controller
            control={control}
            name="category"
            render={({ field }) => (
              <Select onValueChange={field.onChange} value={field.value ?? ''}>
                <SelectTrigger id="category" onBlur={field.onBlur} ref={field.ref}>
                  <SelectValue placeholder="Выберите категорию" />
                </SelectTrigger>
                <SelectContent>
                  {CATEGORIES.map((c) => (
                    <SelectItem key={c.value} value={c.value}>
                      {c.label}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            )}
          />
        </Field>

        <Field label="Город / адрес" error={errors.address?.message} className="md:col-span-2">
          <Input id="address" {...register('address')} placeholder="г. Москва, ул. Примерная, 1" />
        </Field>

        <Field label="Телефон" error={errors.phone?.message}>
          <Input id="phone" {...register('phone')} placeholder="+79001234567" />
        </Field>

        <Field label="Сайт" error={errors.website?.message}>
          <Input id="website" {...register('website')} placeholder="https://example.com" />
        </Field>

        <Field
          label="Описание"
          error={errors.description?.message}
          hint="Что вы делаете и для кого. OneVoice использует это, когда отвечает клиентам и пишет посты."
          className="md:col-span-2"
        >
          <textarea
            id="description"
            {...register('description')}
            rows={4}
            placeholder="Маленькая кофейня у метро. Спешелти, выпекаем круассаны утром."
            className="flex w-full rounded-md border border-line bg-paper-raised px-3 py-2 text-sm text-ink placeholder:text-ink-soft transition-[border-color,box-shadow] duration-150 focus:border-ochre focus:outline-none focus:ring-2 focus:ring-ochre/20 disabled:cursor-not-allowed disabled:opacity-50"
          />
        </Field>
      </div>

      <div className="flex items-center justify-end pt-1">
        <Button
          type="submit"
          variant="primary"
          size="md"
          disabled={isSubmitting || mutation.isPending || !isDirty}
        >
          {isSubmitting || mutation.isPending ? 'Сохраняем…' : 'Сохранить'}
        </Button>
      </div>
    </form>
  );
}

function Field({
  label,
  required,
  error,
  hint,
  className,
  children,
}: {
  label: string;
  required?: boolean;
  error?: string;
  hint?: string;
  className?: string;
  children: React.ReactNode;
}) {
  return (
    <div className={`flex flex-col gap-1.5 ${className ?? ''}`}>
      <Label className="text-xs font-medium text-ink-mid">
        {label}
        {required && <span className="ml-1 text-ochre">*</span>}
      </Label>
      {children}
      {error && <p className="text-xs text-[var(--ov-danger)]">{error}</p>}
      {hint && !error && <p className="text-xs leading-relaxed text-ink-soft">{hint}</p>}
    </div>
  );
}
