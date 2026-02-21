'use client';

import { useEffect } from 'react';
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

  return (
    <form onSubmit={handleSubmit((d) => mutation.mutate(d))} className="space-y-4">
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
