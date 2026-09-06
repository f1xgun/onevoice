import { describe, it, expect } from 'vitest';
import { createLoginSchema, createRegisterSchema } from '../schemas';

// Stub translator — see businessSchema.test.ts for the rationale.
const t = (key: string) => key;
const loginSchema = createLoginSchema(t);
const registerSchema = createRegisterSchema(t);

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

it('normalizes account emails before validating login and registration', () => {
  const email = '  Owner@Example.COM  ';
  expect(loginSchema.parse({ email, password: 'password123' }).email).toBe('owner@example.com');
  expect(
    registerSchema.parse({
      name: 'Owner',
      email,
      password: 'password123',
      confirmPassword: 'password123',
      acceptTosPrivacy: true,
      acceptPdn: true,
    }).email
  ).toBe('owner@example.com');
});
