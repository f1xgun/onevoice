import { describe, it, expect } from 'vitest';
import { loginSchema, registerSchema } from '../schemas';

describe('loginSchema', () => {
  it('rejects empty email', () => {
    const result = loginSchema.safeParse({ email: '', password: 'pass123' });
    expect(result.success).toBe(false);
  });

  it('rejects short password', () => {
    const result = loginSchema.safeParse({ email: 'a@b.com', password: '12' });
    expect(result.success).toBe(false);
  });

  it('accepts valid credentials', () => {
    const result = loginSchema.safeParse({ email: 'a@b.com', password: 'password123' });
    expect(result.success).toBe(true);
  });
});

describe('registerSchema', () => {
  it('rejects mismatched passwords', () => {
    const result = registerSchema.safeParse({
      name: 'Test',
      email: 'a@b.com',
      password: 'password123',
      confirmPassword: 'different',
    });
    expect(result.success).toBe(false);
  });
});
