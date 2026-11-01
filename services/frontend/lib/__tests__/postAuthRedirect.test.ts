import { describe, expect, it } from 'vitest';
import {
  safeNextPath,
  nextParamFrom,
  resolvePostAuthRedirect,
  loginRedirectPath,
} from '../postAuthRedirect';

describe('safeNextPath', () => {
  it('falls back to /inbox for null / undefined / empty', () => {
    expect(safeNextPath(null)).toBe('/inbox');
    expect(safeNextPath(undefined)).toBe('/inbox');
    expect(safeNextPath('')).toBe('/inbox');
  });

  it('accepts a same-origin absolute path (the invite case)', () => {
    expect(safeNextPath('/invite/abc123')).toBe('/invite/abc123');
    expect(safeNextPath('/projects/42/chats')).toBe('/projects/42/chats');
    expect(safeNextPath('/settings/team?tab=invites')).toBe('/settings/team?tab=invites');
  });

  it('rejects protocol-relative / scheme-relative URLs (open-redirect guard)', () => {
    expect(safeNextPath('//evil.com')).toBe('/inbox');
    expect(safeNextPath('/\\evil.com')).toBe('/inbox');
  });

  it('rejects absolute URLs and javascript: payloads', () => {
    expect(safeNextPath('https://evil.com')).toBe('/inbox');
    expect(safeNextPath('http://evil.com/x')).toBe('/inbox');
    expect(safeNextPath('javascript:alert(1)')).toBe('/inbox');
    expect(safeNextPath('evil.com')).toBe('/inbox');
  });

  it('never bounces back to an auth page (loop guard)', () => {
    expect(safeNextPath('/login')).toBe('/inbox');
    expect(safeNextPath('/register')).toBe('/inbox');
    expect(safeNextPath('/login?next=/invite/abc')).toBe('/inbox');
    expect(safeNextPath('/register#x')).toBe('/inbox');
  });
});

describe('nextParamFrom', () => {
  it('extracts a decoded next param', () => {
    expect(nextParamFrom('?next=/invite/abc123')).toBe('/invite/abc123');
    expect(nextParamFrom('?next=%2Finvite%2Fabc')).toBe('/invite/abc');
  });

  it('returns null when next is absent', () => {
    expect(nextParamFrom('')).toBeNull();
    expect(nextParamFrom('?foo=1')).toBeNull();
  });
});

describe('resolvePostAuthRedirect', () => {
  it('routes a valid invite next through, untrusted to /chat', () => {
    expect(resolvePostAuthRedirect('?next=/invite/abc123')).toBe('/invite/abc123');
    expect(resolvePostAuthRedirect('?next=//evil.com')).toBe('/inbox');
    expect(resolvePostAuthRedirect('')).toBe('/inbox');
  });
});

describe('loginRedirectPath', () => {
  it.each(['/chat/shared?message=one&view=full', '/settings/privacy', '/invite/token'])(
    'round trips %s through login',
    (path) => {
      const [pathname, query] = path.split('?');
      const target = loginRedirectPath({ pathname, search: query ? `?${query}` : '' });
      expect(resolvePostAuthRedirect(target.slice(target.indexOf('?')))).toBe(path);
    }
  );
  it('does not create an authentication redirect loop', () => {
    expect(loginRedirectPath({ pathname: '/login', search: '' })).toBe('/login?next=%2Finbox');
  });
});
