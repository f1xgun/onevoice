import { describe, it, expect } from 'vitest';
import { businessSchema } from '../schemas';

describe('businessSchema', () => {
  it('accepts valid business data', () => {
    const result = businessSchema.safeParse({
      name: 'Кофейня Уют',
      category: 'cafe',
      phone: '+79001234567',
      website: 'https://example.com',
      description: 'Уютная кофейня',
    });
    expect(result.success).toBe(true);
  });

  it('rejects empty name', () => {
    const result = businessSchema.safeParse({ name: '', category: 'cafe' });
    expect(result.success).toBe(false);
  });

  it('rejects invalid phone', () => {
    const result = businessSchema.safeParse({
      name: 'Test',
      category: 'cafe',
      phone: 'not-a-phone',
    });
    expect(result.success).toBe(false);
  });

  it('rejects invalid website URL', () => {
    const result = businessSchema.safeParse({
      name: 'Test',
      category: 'cafe',
      website: 'not-a-url',
    });
    expect(result.success).toBe(false);
  });

  it('accepts empty phone (optional)', () => {
    const result = businessSchema.safeParse({ name: 'Test', category: 'cafe', phone: '' });
    expect(result.success).toBe(true);
  });
});
