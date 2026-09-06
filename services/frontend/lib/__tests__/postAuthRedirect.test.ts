import { describe, expect, it } from 'vitest';
import {
  safeNextPath,
  nextParamFrom,
  resolvePostAuthRedirect,
  loginRedirectPath,
} from '../postAuthRedirect';

describe('safeNextPath', () => {
  it('falls back to /chat for null / undefined / empty', () => {
    expect(safeNextPath(null)).toBe('/chat');
    expect(safeNextPath(undefined)).toBe('/chat');
    expect(safeNextPath('')).toBe('/chat');
  });

  it('accepts a same-origin absolute path (the invite case)', () => {
    expect(safeNextPath('/invite/abc123')).toBe('/invite/abc123');
    expect(safeNextPath('/projects/42/chats')).toBe('/projects/42/chats');
    expect(safeNextPath('/settings/team?tab=invites')).toBe('/settings/team?tab=invites');
  });

  it('rejects protocol-relative / scheme-relative URLs (open-redirect guard)', () => {
    expect(safeNextPath('//evil.com')).toBe('/chat');
    expect(safeNextPath('/\\evil.com')).toBe('/chat');
  });

  it('rejects absolute URLs and javascript: payloads', () => {
    expect(safeNextPath('https://evil.com')).toBe('/chat');
    expect(safeNextPath('http://evil.com/x')).toBe('/chat');
    expect(safeNextPath('javascript:alert(1)')).toBe('/chat');
    expect(safeNextPath('evil.com')).toBe('/chat');
  });

  it('never bounces back to an auth page (loop guard)', () => {
    expect(safeNextPath('/login')).toBe('/chat');
    expect(safeNextPath('/register')).toBe('/chat');
    expect(safeNextPath('/login?next=/invite/abc')).toBe('/chat');
    expect(safeNextPath('/register#x')).toBe('/chat');
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
    expect(resolvePostAuthRedirect('?next=//evil.com')).toBe('/chat');
    expect(resolvePostAuthRedirect('')).toBe('/chat');
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
    expect(loginRedirectPath({ pathname: '/login', search: '' })).toBe('/login?next=%2Fchat');
  });
});
