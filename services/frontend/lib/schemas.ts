import { z } from 'zod';

export const loginSchema = z.object({
  email: z.string().email('Некорректный email').max(254),
  password: z.string().min(6, 'Минимум 6 символов'),
});

export const registerSchema = z
  .object({
    name: z.string().min(2, 'Минимум 2 символа').max(100, 'Максимум 100 символов'),
    email: z.string().email('Некорректный email').max(254),
    password: z.string().min(6, 'Минимум 6 символов'),
    confirmPassword: z.string(),
  })
  .refine((d) => d.password === d.confirmPassword, {
    message: 'Пароли не совпадают',
    path: ['confirmPassword'],
  });

export type LoginInput = z.infer<typeof loginSchema>;
export type RegisterInput = z.infer<typeof registerSchema>;

export const businessSchema = z.object({
  name: z.string().min(2, 'Минимум 2 символа').max(200, 'Максимум 200 символов'),
  category: z.string().min(1, 'Выберите категорию'),
  phone: z
    .string()
    .regex(/^\+?[0-9]{7,15}$/, 'Некорректный номер телефона')
    .optional()
    .or(z.literal('')),
  website: z.string().url('Некорректный URL').optional().or(z.literal('')),
  description: z.string().max(500).optional(),
  address: z.string().max(500).optional(),
});

export type BusinessInput = z.infer<typeof businessSchema>;
