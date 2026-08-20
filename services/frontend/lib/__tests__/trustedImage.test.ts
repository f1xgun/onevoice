import { describe, expect, it } from 'vitest';
import { isTrustedImageSrc, safeExternalHref } from '@/lib/trustedImage';

// jsdom serves the tests from http://localhost/, so window.location.host === 'localhost'.

describe('isTrustedImageSrc', () => {
  it('trusts rooted same-origin paths', () => {
    expect(isTrustedImageSrc('/media/photo.png')).toBe(true);
  });

  it('trusts an absolute URL on the current origin', () => {
    expect(isTrustedImageSrc('http://localhost/media/photo.png')).toBe(true);
  });

  it('rejects a cross-origin http(s) URL', () => {
    expect(isTrustedImageSrc('http://attacker.com/track.png')).toBe(false);
    expect(isTrustedImageSrc('https://attacker.com/track.png')).toBe(false);
  });

  it('rejects a protocol-relative URL', () => {
    expect(isTrustedImageSrc('//attacker.com/track.png')).toBe(false);
  });

  it('rejects a backslash-obfuscated authority that WHATWG resolves cross-origin', () => {
    expect(isTrustedImageSrc('/\\attacker.com/track.png')).toBe(false);
    expect(isTrustedImageSrc('/\\\\attacker.com/track.png')).toBe(false);
    expect(isTrustedImageSrc('\\\\attacker.com/track.png')).toBe(false);
  });

  it('rejects non-http schemes and empties', () => {
    expect(isTrustedImageSrc('javascript:alert(1)')).toBe(false);
    expect(isTrustedImageSrc('data:image/png;base64,AAAA')).toBe(false);
    expect(isTrustedImageSrc('')).toBe(false);
  });
});

describe('safeExternalHref', () => {
  it('keeps a rooted same-origin path relative', () => {
    expect(safeExternalHref('/media/photo.png')).toBe('/media/photo.png');
  });

  it('returns an external http(s) URL for a deliberate click-through', () => {
    expect(safeExternalHref('https://example.com/x')).toBe('https://example.com/x');
  });

  it('rejects a backslash-obfuscated authority', () => {
    expect(safeExternalHref('/\\attacker.com/track.png')).toBeUndefined();
  });

  it('rejects javascript: and data: schemes', () => {
    expect(safeExternalHref('javascript:alert(1)')).toBeUndefined();
    expect(safeExternalHref('data:text/html,x')).toBeUndefined();
  });
});
