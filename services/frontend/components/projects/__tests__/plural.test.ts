import { describe, expect, it } from 'vitest';
import { chatsPluralRu } from '@/lib/plural';

describe('chatsPluralRu', () => {
  it.each([
    [0, 'чатов'],
    [1, 'чат'],
    [2, 'чата'],
    [3, 'чата'],
    [4, 'чата'],
    [5, 'чатов'],
    [6, 'чатов'],
    [10, 'чатов'],
    [11, 'чатов'],
    [12, 'чатов'],
    [13, 'чатов'],
    [14, 'чатов'],
    [15, 'чатов'],
    [20, 'чатов'],
    [21, 'чат'],
    [22, 'чата'],
    [24, 'чата'],
    [25, 'чатов'],
    [100, 'чатов'],
    [101, 'чат'],
    [102, 'чата'],
    [111, 'чатов'],
    [112, 'чатов'],
    [121, 'чат'],
  ])('chatsPluralRu(%i) === %s', (n, expected) => {
    expect(chatsPluralRu(n)).toBe(expected);
  });

  it('handles negative numbers by treating absolute value', () => {
    expect(chatsPluralRu(-1)).toBe('чат');
    expect(chatsPluralRu(-5)).toBe('чатов');
  });
});
