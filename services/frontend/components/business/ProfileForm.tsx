'use client';

import { useEffect, useRef, useState } from 'react';
import { useForm, Controller } from 'react-hook-form';
import { zodResolver } from '@hookform/resolvers/zod';
import { useMutation, useQueryClient } from '@tanstack/react-query';
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
import Image from 'next/image';
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
    formState: { errors, isSubmitting },
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
    onError: () => toast.error('Ошибка сохранения'),
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
    onError: () => toast.error('Ошибка загрузки логотипа'),
  });

  function handleFileChange(e: React.ChangeEvent<HTMLInputElement>) {
    const file = e.target.files?.[0];
    if (file) logoMutation.mutate(file);
    e.target.value = '';
  }

  return (
    <form onSubmit={handleSubmit((d) => mutation.mutate(d))} className="space-y-4">
      {/* Logo upload */}
      <div className="flex items-center gap-4">
        <button
          type="button"
          onClick={() => fileInputRef.current?.click()}
          disabled={logoMutation.isPending}
          className="relative h-20 w-20 shrink-0 overflow-hidden rounded-full border-2 border-dashed border-gray-300 bg-gray-50 hover:border-gray-400 focus:outline-none disabled:opacity-50"
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
            <span className="text-2xl text-gray-400">+</span>
          )}
          {logoMutation.isPending && (
            <div className="absolute inset-0 flex items-center justify-center bg-white/70">
              <span className="text-xs text-gray-500">...</span>
            </div>
          )}
        </button>
        <div className="text-sm text-gray-500">
          <p className="font-medium text-gray-700">Логотип</p>
          <p>JPEG, PNG, WebP или GIF · макс. 5 МБ</p>
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
        <div className="space-y-1">
          <Label htmlFor="name">Название *</Label>
          <Input id="name" {...register('name')} placeholder="Кофейня Уют" />
          {errors.name && <p className="text-sm text-red-500">{errors.name.message}</p>}
        </div>

        <div className="space-y-1">
          <Label htmlFor="category">Категория *</Label>
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
          {errors.category && <p className="text-sm text-red-500">{errors.category.message}</p>}
        </div>

        <div className="space-y-1">
          <Label htmlFor="phone">Телефон</Label>
          <Input id="phone" {...register('phone')} placeholder="+79001234567" />
          {errors.phone && <p className="text-sm text-red-500">{errors.phone.message}</p>}
        </div>

        <div className="space-y-1">
          <Label htmlFor="website">Сайт</Label>
          <Input id="website" {...register('website')} placeholder="https://example.com" />
          {errors.website && <p className="text-sm text-red-500">{errors.website.message}</p>}
        </div>

        <div className="space-y-1 md:col-span-2">
          <Label htmlFor="address">Адрес</Label>
          <Input id="address" {...register('address')} placeholder="г. Москва, ул. Примерная, 1" />
          {errors.address && <p className="text-sm text-red-500">{errors.address.message}</p>}
        </div>

        <div className="space-y-1 md:col-span-2">
          <Label htmlFor="description">Описание</Label>
          <Input
            id="description"
            {...register('description')}
            placeholder="Краткое описание бизнеса"
          />
        </div>
      </div>

      <Button type="submit" disabled={isSubmitting || mutation.isPending}>
        {isSubmitting || mutation.isPending ? 'Сохранение...' : 'Сохранить'}
      </Button>
    </form>
  );
}
