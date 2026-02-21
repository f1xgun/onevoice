'use client';

import { useForm } from 'react-hook-form';
import { zodResolver } from '@hookform/resolvers/zod';
import { z } from 'zod';
import { useMutation } from '@tanstack/react-query';
import { toast } from 'sonner';
import { useAuthStore } from '@/lib/auth';
import { api } from '@/lib/api';
import { Button } from '@/components/ui/button';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { Separator } from '@/components/ui/separator';

const passwordSchema = z
  .object({
    currentPassword: z.string().min(1, 'Введите текущий пароль'),
    newPassword: z.string().min(8, 'Минимум 8 символов'),
    confirmPassword: z.string(),
  })
  .refine((d) => d.newPassword === d.confirmPassword, {
    message: 'Пароли не совпадают',
    path: ['confirmPassword'],
  });

type PasswordInput = z.infer<typeof passwordSchema>;

export default function SettingsPage() {
  const user = useAuthStore((s) => s.user);

  const {
    register,
    handleSubmit,
    reset,
    formState: { errors, isSubmitting },
  } = useForm<PasswordInput>({
    resolver: zodResolver(passwordSchema),
  });

  const mutation = useMutation({
    mutationFn: (data: PasswordInput) =>
      api.put('/auth/password', {
        currentPassword: data.currentPassword,
        newPassword: data.newPassword,
      }),
    onSuccess: () => {
      toast.success('Пароль изменён');
      reset();
    },
    onError: () => toast.error('Ошибка смены пароля. Проверьте текущий пароль.'),
  });

  return (
    <div className="max-w-2xl space-y-6 p-8">
      <div>
        <h1 className="mb-1 text-2xl font-bold">Настройки</h1>
        <p className="text-sm text-muted-foreground">Управление аккаунтом</p>
      </div>

      <Card>
        <CardHeader>
          <CardTitle className="text-lg">Аккаунт</CardTitle>
        </CardHeader>
        <CardContent className="space-y-4">
          <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
            <div className="space-y-1">
              <Label className="text-muted-foreground">Имя</Label>
              <p className="text-sm font-medium">{user?.name ?? '—'}</p>
            </div>
            <div className="space-y-1">
              <Label className="text-muted-foreground">Email</Label>
              <p className="text-sm font-medium">{user?.email ?? '—'}</p>
            </div>
            <div className="space-y-1">
              <Label className="text-muted-foreground">Роль</Label>
              <p className="text-sm font-medium capitalize">{user?.role ?? '—'}</p>
            </div>
          </div>
        </CardContent>
      </Card>

      <Separator />

      <Card>
        <CardHeader>
          <CardTitle className="text-lg">Смена пароля</CardTitle>
        </CardHeader>
        <CardContent>
          <form onSubmit={handleSubmit((d) => mutation.mutate(d))} className="space-y-4">
            <div className="space-y-1">
              <Label htmlFor="currentPassword">Текущий пароль</Label>
              <Input id="currentPassword" type="password" {...register('currentPassword')} />
              {errors.currentPassword && (
                <p className="text-sm text-destructive">{errors.currentPassword.message}</p>
              )}
            </div>

            <div className="space-y-1">
              <Label htmlFor="newPassword">Новый пароль</Label>
              <Input id="newPassword" type="password" {...register('newPassword')} />
              {errors.newPassword && (
                <p className="text-sm text-destructive">{errors.newPassword.message}</p>
              )}
            </div>

            <div className="space-y-1">
              <Label htmlFor="confirmPassword">Подтверждение пароля</Label>
              <Input id="confirmPassword" type="password" {...register('confirmPassword')} />
              {errors.confirmPassword && (
                <p className="text-sm text-destructive">{errors.confirmPassword.message}</p>
              )}
            </div>

            <Button type="submit" disabled={isSubmitting || mutation.isPending}>
              {isSubmitting || mutation.isPending ? 'Сохранение...' : 'Изменить пароль'}
            </Button>
          </form>
        </CardContent>
      </Card>
    </div>
  );
}
