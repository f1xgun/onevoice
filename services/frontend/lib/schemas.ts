import { z } from 'zod'

export const loginSchema = z.object({
  email: z.string().email('Некорректный email').max(254),
  password: z.string().min(6, 'Минимум 6 символов'),
})

export const registerSchema = z.object({
  name: z.string().min(2, 'Минимум 2 символа').max(100, 'Максимум 100 символов'),
  email: z.string().email('Некорректный email').max(254),
  password: z.string().min(6, 'Минимум 6 символов'),
  confirmPassword: z.string(),
}).refine((d) => d.password === d.confirmPassword, {
  message: 'Пароли не совпадают',
  path: ['confirmPassword'],
})

export type LoginInput = z.infer<typeof loginSchema>
export type RegisterInput = z.infer<typeof registerSchema>
